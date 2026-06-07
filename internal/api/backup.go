package api

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"movies-api/config"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// GET /api/admin/backup — полный дамп всей БД (pg_dump --clean --if-exists),
// сжатый gzip и отданный файлом. Содержит ВСЁ: пользователей, токены, настройки,
// карточки. Использовать только для переезда на другой сервер.
func handleAPIAdminBackup(w http.ResponseWriter, r *http.Request) {
	dbURL := config.Get().DatabaseURL
	if dbURL == "" {
		http.Error(w, "DATABASE_URL не задан", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	cmd := exec.CommandContext(ctx, "pg_dump", "--clean", "--if-exists", dbURL)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("backup: stdout pipe: %v", err)
		http.Error(w, "не удалось запустить pg_dump", http.StatusInternalServerError)
		return
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		log.Printf("backup: start pg_dump: %v (есть ли postgresql-client в образе?)", err)
		http.Error(w, "pg_dump недоступен", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("movies-backup-%s.sql.gz", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	gz := gzip.NewWriter(w)
	if _, err := io.Copy(gz, stdout); err != nil {
		log.Printf("backup: copy: %v", err)
	}
	_ = gz.Close()

	if err := cmd.Wait(); err != nil {
		// Заголовки уже отправлены — статус сменить нельзя, только логируем.
		log.Printf("backup: pg_dump завершился с ошибкой: %v: %s", err, stderr.String())
	}
}

// POST /api/admin/restore — приём .sql.gz и восстановление через psql.
// ДЕСТРУКТИВНО: заменяет все данные. После успешного восстановления приложение
// перезапускается (сессии/кэши становятся неактуальными после замены БД).
func handleAPIAdminRestore(w http.ResponseWriter, r *http.Request) {
	log.Printf("restore: запрос получен, Content-Length=%d Content-Type=%q", r.ContentLength, r.Header.Get("Content-Type"))

	dbURL := config.Get().DatabaseURL
	if dbURL == "" {
		log.Printf("restore: DATABASE_URL не задан")
		JSON(w, http.StatusInternalServerError, map[string]string{"error": "DATABASE_URL не задан"})
		return
	}

	// До 2 ГБ во временный файл/память.
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		log.Printf("restore: ParseMultipartForm: %v", err)
		JSON(w, http.StatusBadRequest, map[string]string{"error": "не удалось прочитать форму: " + err.Error()})
		return
	}
	file, header, err := r.FormFile("backup")
	if err != nil {
		log.Printf("restore: FormFile(backup): %v", err)
		JSON(w, http.StatusBadRequest, map[string]string{"error": "файл не передан"})
		return
	}
	defer file.Close()
	log.Printf("restore: файл %q, размер %d байт", header.Filename, header.Size)

	gzr, err := gzip.NewReader(file)
	if err != nil {
		log.Printf("restore: gzip.NewReader: %v", err)
		JSON(w, http.StatusBadRequest, map[string]string{"error": "файл не является gzip-архивом"})
		return
	}
	defer gzr.Close()

	ctx := r.Context()
	cmd := exec.CommandContext(ctx, "psql", "-v", "ON_ERROR_STOP=1", dbURL)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		JSON(w, http.StatusInternalServerError, map[string]string{"error": "stdin pipe: " + err.Error()})
		return
	}
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Start(); err != nil {
		JSON(w, http.StatusInternalServerError, map[string]string{"error": "psql недоступен: " + err.Error()})
		return
	}

	// Стримим дамп в psql, попутно вырезая управляющие строки, которые ломают
	// неинтерактивный psql (как в scripts/restore.sh):
	//   • \restrict / \unrestrict — добавляются новыми pg_dump
	//   • set_config('search_path','',false) — иначе хвостовые INSERT-ы не находят
	//     таблицы по неполному имени
	n, pipeErr := streamFilteredDump(gzr, stdin)
	_ = stdin.Close()
	log.Printf("restore: в psql передано %d байт SQL (pipeErr=%v)", n, pipeErr)

	waitErr := cmd.Wait()
	if pipeErr != nil {
		log.Printf("restore: ошибка чтения архива после %d байт: %v; вывод psql: %s", n, pipeErr, tail(out.String(), 800))
		JSON(w, http.StatusInternalServerError, map[string]string{"error": "архив оборвался/повреждён: " + pipeErr.Error()})
		return
	}
	if waitErr != nil {
		log.Printf("restore: psql ошибка: %v: %s", waitErr, out.String())
		JSON(w, http.StatusInternalServerError, map[string]string{"error": "psql: " + tail(out.String(), 800)})
		return
	}

	log.Printf("restore: восстановление из %q завершено, перезапуск", header.Filename)
	JSON(w, http.StatusOK, map[string]bool{"ok": true, "restart": true})
	go func() {
		time.Sleep(800 * time.Millisecond)
		os.Exit(0)
	}()
}

// streamFilteredDump копирует SQL-дамп из r в w построчно, отбрасывая
// управляющие строки psql. Использует bufio.Reader (а не Scanner) — строки
// COPY/INSERT с большими JSON-полями могут превышать лимит Scanner.
func streamFilteredDump(r io.Reader, w io.Writer) (int64, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	bw := bufio.NewWriterSize(w, 1<<20)
	var n int64
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 && !skipDumpLine(line) {
			m, werr := bw.WriteString(line)
			n += int64(m)
			if werr != nil {
				return n, werr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, err
		}
	}
	return n, bw.Flush()
}

func skipDumpLine(line string) bool {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, `\restrict`) || strings.HasPrefix(t, `\unrestrict`) {
		return true
	}
	if strings.Contains(t, "set_config('search_path', '', false)") {
		return true
	}
	return false
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
