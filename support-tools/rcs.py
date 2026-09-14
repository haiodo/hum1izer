# Достаёт head-ревизию из RCS ,v: она лежит целиком в первом блоке text,
# остальные ревизии - обратные дельты. Внутри @...@ символ @ удвоен.
import os, sys

def body(raw):
    i = raw.find('\ndesc\n')
    if i < 0:
        return None
    i = raw.find('\ntext\n@', i)
    if i < 0:
        return None
    i += len('\ntext\n@')
    out = []
    while i < len(raw):
        j = raw.find('@', i)
        if j < 0:
            break
        out.append(raw[i:j])
        if j + 1 < len(raw) and raw[j + 1] == '@':
            out.append('@')
            i = j + 2
            continue
        break
    return ''.join(out)

src, dst = sys.argv[1], sys.argv[2]
n = 0
for dp, dn, fn in os.walk(src):
    for f in fn:
        if not f.endswith('.java,v'):
            continue
        p = os.path.join(dp, f)
        raw = open(p, encoding='utf-8', errors='replace').read()
        t = body(raw)
        if not t:
            continue
        rel = os.path.relpath(dp, src).replace('/Attic', '')
        out = os.path.join(dst, rel)
        os.makedirs(out, exist_ok=True)
        open(os.path.join(out, f[:-2]), 'w', encoding='utf-8').write(t)
        n += 1
print('извлечено файлов:', n)
