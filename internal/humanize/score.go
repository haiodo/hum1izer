package humanize

import "fmt"

// Score — чистота 0-100: не вероятность ИИ, а агрегат метрик для сравнения
// «было / стало». Веса перенесены из humanizer_metrics/score.py, кроме пункта 6.
type Score struct {
	Value     int       `json:"score"`
	Band      string    `json:"band"`
	Penalties []Penalty `json:"penalties"`
	Notes     []string  `json:"notes,omitempty"`
}

type Penalty struct {
	Reason string `json:"reason"`
	Points int    `json:"points"`
}

const sterileMinWords = 100

// humanZeroShare: доля людей, у которых текст такой длины совсем без маркеров.
// Измерено авторами оригинала на 14 973 человеческих текстах.
var humanZeroShare = []struct {
	Limit int
	Share float64
}{{100, 42.2}, {200, 21.0}, {400, 15.1}}

func band(v int, t Thresholds) string {
	switch {
	case v >= t.BandClean:
		return "чисто"
	case v >= t.BandEdit:
		return "правка"
	default:
		return "рерайт"
	}
}

func per100(count, words int) float64 {
	if words == 0 {
		return 0
	}
	return float64(count) / float64(words) * 100
}

func cleanliness(rs *RuleSet, r Report, dashMuted bool) Score {
	t := rs.Thresholds
	words := r.Rhythm.Words
	if words == 0 {
		words = 1
	}
	s := Score{}
	score := 100.0
	add := func(reason string, pts int) {
		score -= float64(pts)
		s.Penalties = append(s.Penalties, Penalty{Reason: reason, Points: -pts})
	}

	// 1. Фразовые хард-баны, кроме тире: однозначные AI-обороты, дорого.
	hardPhrase := 0
	for _, h := range r.HardBans {
		if h.Marker != "Длинное тире" {
			hardPhrase += h.Count
		}
	}
	if hardPhrase > 0 {
		add(fmt.Sprintf("хард-баны (фразы): %d", hardPhrase), min(45, 12*hardPhrase))
	}

	// 2. Артефакты копипасты: текст буквально вставлен из ответа чат-бота.
	copyPaste := 0
	for _, h := range r.Markers {
		if h.Category == rs.CopyPasteCategory {
			copyPaste += h.Count
		}
	}
	if copyPaste > 0 {
		add(fmt.Sprintf("артефакты копипасты: %d", copyPaste), 60)
	}

	// 3. Мягкие маркеры по плотности на 100 слов.
	soft := 0
	for _, h := range r.Markers {
		if h.Category != rs.CopyPasteCategory {
			soft += h.Count
		}
	}
	if soft > 0 {
		if pen := min(30, roundInt(2*per100(soft, words))); pen > 0 {
			add(fmt.Sprintf("маркеры: %d (%.1f/100 слов)", soft, per100(soft, words)), pen)
		}
	}

	// 4. Длинное тире по плотности с допуском ~2 на 100 слов: в русском оно штатно.
	dashDensity := 0.0
	if !dashMuted {
		dashDensity = per100(r.Rhythm.EmDash, words)
	}
	if dashDensity > 2.0 {
		if pen := min(8, roundInt(3*(dashDensity-2.0))); pen > 0 {
			add(fmt.Sprintf("тире: %d (%.1f/100 слов)", r.Rhythm.EmDash, dashDensity), pen)
		}
	}

	// 5. Ровный ритм: чем ниже CV относительно цели, тем больше штраф.
	if r.Rhythm.Sentences >= 4 && r.Rhythm.CVLen < t.CVHumanTarget {
		pen := min(20, roundInt((t.CVHumanTarget-r.Rhythm.CVLen)/t.CVHumanTarget*30))
		if pen > 0 {
			add(fmt.Sprintf("ровный ритм (CV=%.3f, цель ≥%.2f)", r.Rhythm.CVLen, t.CVHumanTarget), pen)
		}
	}

	// 6. Номинальность. Оригинал берёт сущ./глаг. из pymorphy3; здесь плотность
	//    отглагольных существительных. Сигнал тот же (канцелярит), вес тот же.
	if r.Morph.Per100 > t.NominalTarget {
		pen := min(8, roundInt((r.Morph.Per100-t.NominalTarget)/1.5*3))
		if pen > 0 {
			add(fmt.Sprintf("номинальность (отглагольных сущ. %.1f/100 слов, цель ≤%.1f)",
				r.Morph.Per100, t.NominalTarget), pen)
		}
	}

	// 7. Ровные по длине абзацы. Инертно на короткой прозе.
	st := r.Structure
	if st.Paragraphs >= t.ParaMinCount && st.ParaCV < t.ParaCVAI {
		pen := min(10, roundInt((t.ParaCVAI-st.ParaCV)/t.ParaCVAI*20))
		if pen > 0 {
			add(fmt.Sprintf("ровные абзацы (CV=%.3f, цель ≥%.2f)", st.ParaCV, t.ParaCVAI), pen)
		}
	}

	// 8. Listicle-сигнатура: засилье однотипных пунктов.
	if st.ListItems >= t.ListicleMinItems && st.ListicleShare > t.ListicleShareAI {
		pen := min(12, roundInt((st.ListicleShare-t.ListicleShareAI)*30))
		if pen > 0 {
			add(fmt.Sprintf("листикл (%d пунктов, %d%% строк)",
				st.ListItems, int(st.ListicleShare*100)), pen)
		}
	}

	// Заметка о стерильности: ноль маркеров это не середина человеческого
	// распределения, а его редкий край. Штрафа нет, чтобы не ломать «было/стало».
	if hardPhrase == 0 && copyPaste == 0 && soft == 0 && r.Rhythm.Words >= sterileMinWords {
		share := humanZeroShare[len(humanZeroShare)-1].Share
		for _, h := range humanZeroShare {
			if r.Rhythm.Words < h.Limit {
				share = h.Share
				break
			}
		}
		s.Notes = append(s.Notes, fmt.Sprintf(
			"стерильно: ни одного маркера. Так пишет %.0f%% людей на тексте в %d слов, "+
				"остальные %.0f%% что-нибудь да используют. Цель не ноль, а типичная частота",
			share, r.Rhythm.Words, 100-share))
	}

	s.Value = max(0, min(100, roundInt(score)))
	s.Band = band(s.Value, t)
	return s
}

func roundInt(v float64) int {
	if v < 0 {
		return -int(-v + 0.5)
	}
	return int(v + 0.5)
}

// MarkerVerdict — шкала из SKILL.md: 0-2 чисто, 3-5 подозрительно, 6+ AI.
func MarkerVerdict(rs *RuleSet, hits []Hit) string {
	total := 0
	for _, h := range hits {
		if h.Category == rs.CopyPasteCategory {
			return "артефакты копипасты из чат-бота — текст вставлен из ответа ИИ"
		}
		total += h.Count
	}
	switch {
	case total <= 2:
		return fmt.Sprintf("%d — скорее всего чистый текст", total)
	case total <= 5:
		return fmt.Sprintf("%d — подозрительно", total)
	default:
		return fmt.Sprintf("%d — AI-генерация с высокой вероятностью", total)
	}
}
