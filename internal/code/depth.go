package code

import "strings"

// maxDepth - предел вложенности. Пять, а не четыре: отступы завышают глубину
// вдвое, gofmt даёт switch два уровня там, где разбор дерева видит один.
const maxDepth = 5

// indentOf - ширина отступа в пробелах, таб за восемь.
func indentOf(l string) int {
	n := 0
	for _, ch := range l {
		switch ch {
		case '\t':
			n += 8
		case ' ':
			n++
		default:
			return n
		}
	}
	return n
}

// indentUnit - шаг отступа файла: самая частая разница между уровнями. По
// минимуму не выйдет: выравнивание продолжений даёт разрывы в один-два пробела.
func indentUnit(lines []string) int {
	seen, prev := map[int]int{}, 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if i := indentOf(l); i > prev {
			seen[i-prev]++
		} else {
			prev = i
			continue
		}
		prev = indentOf(l)
	}
	best, n := 4, 0
	for _, step := range []int{2, 4, 8} {
		if seen[step] > n {
			best, n = step, seen[step]
		}
	}
	return best
}

// blockDepth - вложенность функции после строки after, нумерация с единицы.
// Ноль, если там не функция.
func blockDepth(lines []string, after int) int {
	i := after
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || !strings.Contains(lines[i], "(") {
		return 0
	}
	unit := indentUnit(lines)
	base, deep := indentOf(lines[i]), indentOf(lines[i])
	for j := i + 1; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		if t == "" {
			continue
		}
		ind := indentOf(lines[j])
		if ind <= base {
			break
		}
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") {
			continue
		}
		if ind > deep {
			deep = ind
		}
	}
	if d := (deep-base)/unit - 1; d > 0 {
		return d
	}
	return 0
}
