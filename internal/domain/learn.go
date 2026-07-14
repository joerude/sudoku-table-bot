package domain

import "math/rand"

// Tier groups techniques by how deep into a puzzle you need them.
type Tier string

const (
	TierBasic Tier = "basic"
	TierMid   Tier = "mid"
	TierAdv   Tier = "adv"
)

// Technique is one entry of the /learn dictionary: a short RU explanation plus
// links out. Static data — no DB, no network at request time.
type Technique struct {
	Key   string // ascii, callback-safe
	Name  string // RU
	Alias string // EN — what to google
	Tier  Tier
	Blurb string // RU, 2-4 sentences
	URL   string // main article
	Wiki  string // optional deeper article on sudokuwiki.org
}

// Tiers lists tiers in learning order.
func Tiers() []Tier { return []Tier{TierBasic, TierMid, TierAdv} }

const wikiIntersection = "https://www.sudokuwiki.org/intersection_removal"

// techniques is the whole dictionary; links verified 2026-07-14.
var techniques = []Technique{
	// --- Базовые ---
	{
		Key: "obvious-single", Name: "Голая одиночка", Alias: "Obvious (Naked) Single", Tier: TierBasic,
		Blurb: "В клетке осталась ровно одна возможная цифра: остальные восемь уже стоят в её строке, столбце или блоке. Ставь и не думай. Пока такие клетки есть, ничего сложнее искать не нужно.",
		URL:   "https://sudoku.com/sudoku-rules/obvious-singles/",
	},
	{
		Key: "hidden-single", Name: "Скрытая одиночка", Alias: "Hidden Single", Tier: TierBasic,
		Blurb: "В строке, столбце или блоке цифра помещается только в одну клетку — даже если кандидатов в самой клетке несколько. Смотри не на клетку, а на цифру: где ещё она может стоять? Если больше негде — она там.",
		URL:   "https://sudoku.com/sudoku-rules/hidden-singles/",
	},
	{
		Key: "obvious-pair", Name: "Голая пара", Alias: "Obvious (Naked) Pair", Tier: TierBasic,
		Blurb: "Две клетки области содержат одну и ту же пару кандидатов и ничего больше. Эти две цифры забронированы за ними — вычёркивай их из всех остальных клеток области.",
		URL:   "https://sudoku.com/sudoku-rules/obvious-pairs/",
	},
	{
		Key: "hidden-pair", Name: "Скрытая пара", Alias: "Hidden Pair", Tier: TierBasic,
		Blurb: "Две цифры встречаются в области только в двух клетках — значит, займут именно их. Все прочие кандидаты из этих двух клеток убираются, даже если их там было по пять.",
		URL:   "https://sudoku.com/sudoku-rules/hidden-pairs/",
	},
	{
		Key: "pointing-pair", Name: "Указывающая пара", Alias: "Pointing Pair", Tier: TierBasic,
		Blurb: "В блоке кандидат X стоит только в двух клетках, и обе лежат на одной строке (или столбце). Значит X точно в этом блоке и на этой линии — вычёркивай X из остальной части линии за пределами блока.",
		URL:   "https://sudoku.com/sudoku-rules/pointing-pairs/",
		Wiki:  wikiIntersection,
	},
	{
		Key: "pointing-triple", Name: "Указывающая тройка", Alias: "Pointing Triple", Tier: TierBasic,
		Blurb: "То же, что указывающая пара, только кандидат занимает три клетки блока на одной линии. Линия за пределами блока чистится от этого кандидата.",
		URL:   "https://sudoku.com/sudoku-rules/pointing-triples/",
		Wiki:  wikiIntersection,
	},

	// --- Средние ---
	{
		Key: "obvious-triple", Name: "Голая тройка", Alias: "Obvious (Naked) Triple", Tier: TierMid,
		Blurb: "Три клетки области делят между собой три кандидата — у каждой клетки может быть две или три из них. Тройка запирает цифры за собой: из остальных клеток области они уходят.",
		URL:   "https://sudoku.com/sudoku-rules/obvious-triples/",
	},
	{
		Key: "hidden-triple", Name: "Скрытая тройка", Alias: "Hidden Triple", Tier: TierMid,
		Blurb: "Три цифры встречаются в области только в трёх клетках — все лишние кандидаты из этих клеток вычёркиваются. Заметить труднее, чем голую тройку: сканируй по цифрам, а не по клеткам.",
		URL:   "https://sudoku.com/sudoku-rules/hidden-triples/",
	},
	{
		Key: "box-line", Name: "Блок-линия", Alias: "Box/Line Reduction", Tier: TierMid,
		Blurb: "Зеркало указывающей пары. Если в строке (или столбце) кандидат X остался только внутри одного блока, то X точно на этом пересечении — убирай X из остальных клеток блока.",
		URL:   wikiIntersection,
	},
	{
		Key: "x-wing", Name: "X-Wing", Alias: "X-Wing", Tier: TierMid,
		Blurb: "Кандидат X в двух строках стоит ровно в двух клетках, и эти клетки — в одних и тех же столбцах: получается прямоугольник. Как ни ложись цифры, X займёт два противоположных угла, поэтому X вычёркивается из этих столбцов везде, кроме прямоугольника. Работает и наоборот: столбцы → строки.",
		URL:   "https://sudoku.com/sudoku-rules/h-wing/", // да, слаг такой — опечатка на sudoku.com
		Wiki:  "https://www.sudokuwiki.org/x_wing_strategy",
	},
	{
		Key: "y-wing", Name: "Y-Wing", Alias: "Y-Wing (XY-Wing)", Tier: TierMid,
		Blurb: "Три клетки с двумя кандидатами: опора AB видит клетки AC и BC. Какой бы цифрой ни оказалась опора, одна из клешней станет C — значит, любая клетка, которую видят обе клешни, теряет кандидата C.",
		URL:   "https://sudoku.com/sudoku-rules/y-wing/",
		Wiki:  "https://www.sudokuwiki.org/y_wing_strategy",
	},
	{
		Key: "swordfish", Name: "Swordfish", Alias: "Swordfish", Tier: TierMid,
		Blurb: "X-Wing на трёх строках вместо двух: кандидат X в трёх строках умещается максимум в три столбца. Тогда X уходит из этих столбцов во всех остальных строках. Полного заполнения не требуется — хватает двух-трёх позиций в строке.",
		URL:   "https://sudoku.com/sudoku-rules/swordfish/",
	},

	// --- Продвинутые (на sudoku.com их нет — только sudokuwiki) ---
	{
		Key: "xyz-wing", Name: "XYZ-Wing", Alias: "XYZ-Wing", Tier: TierAdv,
		Blurb: "Как Y-Wing, но опора держит три кандидата XYZ, а клешни — XZ и YZ. Кандидат Z снимается только с клеток, которые видят все три клетки конструкции, включая саму опору.",
		URL:   "https://www.sudokuwiki.org/XYZ_Wing",
	},
	{
		Key: "jellyfish", Name: "Jellyfish", Alias: "Jellyfish", Tier: TierAdv,
		Blurb: "Ступень после Swordfish: четыре строки, четыре столбца. Кандидат X в четырёх строках умещается в четыре столбца — чистим эти столбцы в остальных строках. Встречается редко: обычно раньше находится что-то попроще.",
		URL:   "https://www.sudokuwiki.org/Jelly_Fish_Strategy",
	},
	{
		Key: "simple-coloring", Name: "Простая раскраска", Alias: "Simple Colouring", Tier: TierAdv,
		Blurb: "Берём цифру, у которой в областях ровно по две позиции, и красим цепочку в два цвета: соседи по звену всегда разного цвета. Если один цвет дважды попал в одну область — он ложный, ставим весь другой. Клетки, видящие оба цвета, теряют кандидата.",
		URL:   "https://www.sudokuwiki.org/Simple_Colouring",
	},
	{
		Key: "singles-chain", Name: "Цепочки одиночек", Alias: "Singles Chains", Tier: TierAdv,
		Blurb: "Формализованная двухцветная логика: сильные связи (ровно две позиции в области) чередуются, и правила раскраски снимают кандидатов. Основа всех цепочечных техник — дальше идут 3D Medusa и X-Cycles.",
		URL:   "https://www.sudokuwiki.org/Singles_Chains",
	},
	{
		Key: "xy-chain", Name: "XY-Chains", Alias: "XY-Chains", Tier: TierAdv,
		Blurb: "Цепочка клеток с двумя кандидатами, где соседние звенья делят общую цифру. Если оба конца цепочки могут оказаться Z, то любая клетка, видящая оба конца, теряет Z. Продолжение Y-Wing на произвольную длину.",
		URL:   "https://www.sudokuwiki.org/XY_Chains",
	},
	{
		Key: "unique-rect", Name: "Уникальный прямоугольник", Alias: "Unique Rectangle", Tier: TierAdv,
		Blurb: "У корректной судоку решение единственное. Если четыре клетки в двух строках, двух столбцах и двух блоках грозят остаться с одной и той же парой кандидатов, решений было бы два — такая расстановка запрещена, и лишний кандидат снимается. На кривых судоку с несколькими решениями не работает.",
		URL:   "https://www.sudokuwiki.org/Unique_Rectangles",
	},
}

// Techniques returns the whole dictionary in learning order.
func Techniques() []Technique {
	out := make([]Technique, len(techniques))
	copy(out, techniques)
	return out
}

// TechniquesByTier returns the six techniques of one tier, nil for an unknown tier.
func TechniquesByTier(t Tier) []Technique {
	var out []Technique
	for _, tech := range techniques {
		if tech.Tier == t {
			out = append(out, tech)
		}
	}
	return out
}

// TechniqueByKey looks a technique up by its callback key.
func TechniqueByKey(key string) (Technique, bool) {
	for _, tech := range techniques {
		if tech.Key == key {
			return tech, true
		}
	}
	return Technique{}, false
}

// RandomTechnique picks one at random; the caller owns the source, so tests stay
// deterministic.
func RandomTechnique(r *rand.Rand) Technique {
	return techniques[r.Intn(len(techniques))]
}
