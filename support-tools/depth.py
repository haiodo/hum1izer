# Форма функций по отступам: работает там, где парсера нет (Swift, Kotlin).
# Функция - строка с func/fn на любом отступе, тело - всё, что отступлено глубже.
import os, re, sys

SIG = re.compile(r'^\s*((public|private|internal|fileprivate|open|static|final|override|func|def)\s)*func\s|^func[ (]')

def indent(l):
    n = 0
    for ch in l:
        if ch == '\t':
            n += 8
        elif ch == ' ':
            n += 1
        else:
            break
    return n

def unit(lines):
    # Шаг отступа файла - самая частая разница, а не самая маленькая:
    # выравнивание продолжений строки даёт разрывы в 1-2 пробела и портит минимум.
    from collections import Counter
    c = Counter()
    prev = 0
    for l in lines:
        if not l.strip():
            continue
        i = indent(l)
        if i > prev:
            c[i - prev] += 1
        prev = i
    for step, _ in c.most_common():
        if step in (2, 4, 8):
            return step
    return 4

def scan(paths):
    out = []
    for p in paths:
        try:
            lines = open(p, encoding='utf-8', errors='replace').read().split('\n')
        except OSError:
            continue
        u = unit(lines)
        i = 0
        while i < len(lines):
            if not SIG.match(lines[i]):
                i += 1
                continue
            base = indent(lines[i])
            deep, body, j = base, 0, i + 1
            while j < len(lines):
                l = lines[j]
                if not l.strip():
                    j += 1
                    continue
                if indent(l) <= base and not l.lstrip().startswith(('}', ')', ']')):
                    break
                if indent(l) <= base and l.lstrip().startswith('}'):
                    break
                t = l.strip()
                if not t.startswith(('//', '*', '/*')):
                    body += 1
                    deep = max(deep, indent(l))
                j += 1
            out.append(((deep - base) // u - 1, body))
            i = j
    return out

def report(label, paths):
    ms = [m for m in scan(paths) if m[1] > 0]
    if not ms:
        return
    d = sorted(max(x, 0) for x, _ in ms)
    s = sorted(y for _, y in ms)
    n = len(ms)
    p = lambda v, q: v[min(len(v) * q // 100, len(v) - 1)]
    od = sum(1 for x in d if x > 4) * 100 / n
    os_ = sum(1 for x in s if x > 60) * 100 / n
    if os.getenv('CSV'):
        print(f"{label},{n},{p(d,50)},{p(d,90)},{p(d,95)},{p(d,99)},"
              f"{p(s,50)},{p(s,90)},{p(s,95)},{p(s,99)},{od:.2f},{os_:.2f}")
        return
    print(f"{label:<26} функций {n:5d}  depth {p(d,50)}/{p(d,90)}/{p(d,95)}/{p(d,99)}  "
          f"sloc {p(s,50)}/{p(s,90)}/{p(s,95)}/{p(s,99)}  "
          f"depth>4 {od:4.1f}%  sloc>60 {os_:4.1f}%")

if __name__ == '__main__':
    for root in sys.argv[1:]:
        if os.path.isfile(root):
            report(os.path.basename(root), [l.strip() for l in open(root) if l.strip()])
            continue
        ps = []
        for dp, dn, fn in os.walk(root):
            dn[:] = [x for x in dn if x not in ('build', '.git', 'node_modules', 'vendor', 'Pods', 'bin')]
            ps += [os.path.join(dp, f) for f in fn if f.endswith(('.swift', '.go', '.ts', '.kt'))
                   and not f.endswith(('_test.go', '.d.ts'))]
        report(os.path.basename(root.rstrip('/')), ps)
