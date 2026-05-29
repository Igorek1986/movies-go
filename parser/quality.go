package parser

import (
	"strings"

	"movies-api/db/models"
)

func ParseVQuality(params string) int {
	info := clear(strings.ToLower(params))
	info = strings.ReplaceAll(info, "вdrip", "bdrip")
	info = strings.ReplaceAll(info, "web dl", "webdl")

	if strings.Contains(info, "2160") && (strings.Contains(info, "bdremux") || strings.Contains(info, "bluray")) {
		if strings.Contains(info, "dolby vision") {
			return models.Q_UHD_BDREMUX_DV
		} else if strings.Contains(info, "hdr") {
			return models.Q_UHD_BDREMUX_HDR
		} else {
			return models.Q_UHD_BDREMUX_SDR
		}
	}
	if strings.Contains(info, "2160") && strings.Contains(info, "bdrip") {
		if strings.Contains(info, "dolby vision") {
			return models.Q_BDRIP_DV_2160
		} else if strings.Contains(info, "hdr") {
			return models.Q_BDRIP_HDR_2160
		} else {
			return models.Q_BDRIP_SDR_2160
		}
	}
	if strings.Contains(info, "2160") && (strings.Contains(info, "webdl") || strings.Contains(info, "webrip")) {
		if strings.Contains(info, "dolby vision") {
			return models.Q_WEBDL_DV_2160
		} else if strings.Contains(info, "hdr") {
			return models.Q_WEBDL_HDR_2160
		} else {
			return models.Q_WEBDL_SDR_2160
		}
	}
	if strings.Contains(info, "1080") && (strings.Contains(info, "remux") || strings.Contains(info, "bluray")) {
		return models.Q_BDREMUX_1080
	}
	if strings.Contains(info, "1080") && strings.Contains(info, "bdrip") && strings.Contains(info, "hevc") {
		return models.Q_BDRIP_HEVC_1080
	}
	if strings.Contains(info, "1080") && strings.Contains(info, "bdrip") {
		return models.Q_BDRIP_1080
	}
	if strings.Contains(info, "1080") && (strings.Contains(info, "webdl") || strings.Contains(info, "webrip") || strings.Contains(info, "hdrip") || strings.Contains(info, "hybrid")) {
		return models.Q_WEBDL_1080
	}
	if strings.Contains(info, "720") && strings.Contains(info, "bdrip") && strings.Contains(info, "hevc") {
		return models.Q_BDRIP_HEVC_720
	}
	if strings.Contains(info, "720") && strings.Contains(info, "bdrip") {
		return models.Q_BDRIP_720
	}
	if strings.Contains(info, "720") && (strings.Contains(info, "webdl") || strings.Contains(info, "webrip") || strings.Contains(info, "dvd") || strings.Contains(info, "hdrip")) {
		return models.Q_WEBDL_720
	}
	return models.Q_LOWER
}

func ParseAQuality(params string) int {
	arr := strings.Split(params, "|")
	var qualities []int
	for _, name := range arr {
		name = clear(name)
		for _, qn := range Q_Lic_Names {
			if strings.Contains(name, clear(qn)) {
				qualities = append(qualities, models.Q_LICENSE)
			}
		}
		for _, qn := range Q_P_Names {
			if !strings.Contains(qn, " ") {
				for _, s := range strings.Split(clear(name), " ") {
					if s == clear(qn) {
						qualities = append(qualities, models.Q_PS)
					}
				}
			} else {
				if strings.Contains(name, clear(qn)) {
					qualities = append(qualities, models.Q_PS)
				}
			}
		}
		for _, qn := range Q_L_Names {
			if strings.Contains(name, clear(qn)) {
				qualities = append(qualities, models.Q_LS)
			}
		}
		for _, w := range strings.Split(name, " ") {
			w = strings.TrimSpace(w)
			switch w {
			case "d":
				qualities = append(qualities, models.Q_D)
			case "p":
				qualities = append(qualities, models.Q_P)
			case "p2":
				qualities = append(qualities, models.Q_P2)
			case "p1":
				qualities = append(qualities, models.Q_P1)
			case "l":
				qualities = append(qualities, models.Q_L)
			case "l2":
				qualities = append(qualities, models.Q_L2)
			case "l1":
				qualities = append(qualities, models.Q_L1)
			case "a":
				qualities = append(qualities, models.Q_A)
			}
		}
	}

	if len(qualities) == 0 {
		return models.Q_UNKNOWN
	}
	max := models.Q_UNKNOWN
	for _, q := range qualities {
		if q > max {
			max = q
		}
	}
	return max
}

func clear(txt string) string {
	txt = strings.ToLower(txt)
	var b strings.Builder
	for _, r := range txt {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'а' && r <= 'я') || r == 'ё' || r == ' ' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var Q_Lic_Names = []string{
	"лицензия",
	"itunes",
	"netflix",
}

var Q_P_Names = []string{
	"100ТВ",
	"2х2",
	"Agatha Studdio",
	"AlexFilm",
	"Amedia",
	"NovaFilm",
	"Novamedia",
	"AMS",
	"ARS-studio",
	"Astana TV",
	"AzOnFilm",
	"AXN Sci-Fi",
	"CDV",
	"CGInfo",
	"CP Digital",
	"Disney",
	"DniproFilm",
	"DVDXpert",
	"Elrom",
	"Filiza Studio",
	"Flarrow Films",
	"FocusX",
	"FocusStudio",
	"FOXCrime",
	"FoxLife",
	"Gears Media",
	"Good People",
	"HDrezka Studio",
	"IdeaFilm",
	"IVI",
	"Jaskier",
	"Kansai Studio",
	"LostFilm",
	"MC Entertaiment",
	"Mega-Anime",
	"MTV",
	"Neoclassica",
	"NewComers",
	"NewStudio",
	"Nickelodeon",
	"NovaFilm",
	"NovaMedia",
	"Ozz",
	"Paramount",
	"Profix Media",
	"Rattlebox",
	"SDI Media",
	"Sony Sci-Fi",
	"Superbit",
	"TUMBLER Studio",
	"TVShows",
	"FilmsClub",
	"Tycoon",
	"Universal",
	"ViruseProject",
	"WestVideo",
	"Арена",
	"Арк-ТВ",
	"Воротилин",
	"Домашний",
	"ДТВ",
	"ДубльPR studio",
	"Екатеринбург-Арт",
	"Инис",
	"Лексикон",
	"Киномания",
	"Кипарис",
	"Кириллица",
	"Кравец",
	"Кубик в Кубе",
	"Кураж Бомбей",
	"Невафильм",
	"Новый канал",
	"НТВ",
	"НТВ+",
	"Омикрон",
	"ОРТ",
	"Парадиз-ВС",
	"Первый канал",
	"Петербург 5 канал",
	"Пифагор",
	"Позитив-Мультимедиа",
	"Премьер Видео Фильм",
	"РЕН-ТВ",
	"С.Р.И.",
	"Специальное Российское Издание",
	"СВ-Студия",
	"СоюзВидео",
	"студия «Велес»",
	"студия «Нота»",
	"студия «СВ Дубль»",
	"ТВ3",
	"ТВЦ",
	"ТНТ",
	"ТОО Прим",
	"Хабар",
	"Pazl Voice",
}

var Q_L_Names = []string{
	"Albion Studio",
	"Alternative Production",
	"AniDub",
	"AniFilm",
	"AniLibria",
	"Anilife Project",
	"AniMedia",
	"AnimeReactor",
	"AnimeVost",
	"AniPlay",
	"AniStar",
	"ApofysTeam",
	"Baibako",
	"BraveSound",
	"CACTUS TEAM",
	"СoldFilm",
	"DexterTV",
	"DreamRecords",
	"Eleonor Film",
	"E-Production",
	"Etvox Film",
	"Filiza Studio",
	"Flux-Team",
	"F-TRAIN",
	"GladiolusTV",
	"GostFilm",
	"Gramalant",
	"GREEN TEA",
	"GSGroup",
	"HamsterStudio",
	"ICG",
	"Jetvis Studio",
	"Jimmy J",
	"LevshaFilm",
	"LugaDUB",
	"LE-Production",
	"Mallorn Studio",
	"MYDIMKA",
	"Naruto-Hokage",
	"NetLab Anima Group",
	"NewStation",
	"NikiStudio Records",
	"OmskBird",
	"OneFilm",
	"OpenDub",
	"Padabajour",
	"ParadoX",
	"RG.Paravozik",
	"Shiza Project",
	"SkyeFilmTV",
	"STEPonee",
	"StopGame",
	"Sunny-Films",
	"To4kaTV",
	"VictoryFilms",
	"Web_Money",
	"ZM-SHOW",
	"Несмертельное оружие",
	"Причудики",
	"Райдо",
	"Синема-УС",
	"Студия Пиратского Дубляжа",
	"Сладкая парочка",
	"Частная Студия",
}
