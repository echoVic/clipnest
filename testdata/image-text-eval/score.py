"""Score the results TestImageTextEvaluation writes into the corpus.

    python3 testdata/image-text-eval/score.py <corpus> [-v]

Recall is the share of search queries found in the stored text as
case-insensitive substrings, the way Clipnest search matches. CER is the
character error rate with whitespace removed. Images without truth lines
(photos) report how many returned any text. -v lists missed queries.
"""
import json
import os
import statistics
import sys


def cer(hyp, ref):
    a, b = ''.join(hyp.split()), ''.join(ref.split())
    prev = list(range(len(b) + 1))
    for i, ca in enumerate(a, 1):
        cur = [i]
        for j, cb in enumerate(b, 1):
            cur.append(min(prev[j] + 1, cur[j - 1] + 1, prev[j - 1] + (ca != cb)))
        prev = cur
    return prev[-1] / max(1, len(b))


def score(path, verbose):
    truth = {t['name']: t for t in json.load(open(os.path.join(path, 'truth.json')))}
    results = [json.loads(line) for line in open(os.path.join(path, 'results.jsonl'))]
    if {r['name'] for r in results} != set(truth):
        sys.exit(f'{path}: results.jsonl does not match truth.json')
    ms = [r['ms'] for r in results]
    timing = f'median {statistics.median(ms):.0f} ms, total {sum(ms) / 1000:.1f} s'
    errors = [r['name'] for r in results if r.get('error')]
    label = f'{os.path.basename(path)}: {len(results)} images'
    if errors:
        label += f', {len(errors)} failed ({", ".join(errors)})'
    lined = [r for r in results if truth[r['name']]['lines']]
    if not lined:
        found = [r['name'] for r in results if r['text']]
        print(f'{label}, {len(found)} with text {found or ""}, {timing}')
        return
    hits = total = 0
    missed = []
    for r in lined:
        text = r['text'].lower()
        for q in truth[r['name']]['queries']:
            total += 1
            if q.lower() in text:
                hits += 1
            else:
                missed.append(f'{r["name"]}: {q}')
    rate = statistics.mean(cer(r['text'], '\n'.join(truth[r['name']]['lines'])) for r in lined)
    print(f'{label}, recall {hits / total:.1%} ({hits}/{total}), CER {rate:.3f}, {timing}')
    if verbose:
        for m in missed:
            print('    miss', m)


def main():
    args = [a for a in sys.argv[1:] if a != '-v']
    if len(args) != 1:
        sys.exit(__doc__)
    sets = [os.path.join(args[0], d) for d in sorted(os.listdir(args[0]))]
    sets = [d for d in sets if os.path.exists(os.path.join(d, 'results.jsonl'))]
    if not sets:
        sys.exit(f'no results.jsonl under {args[0]}; run TestImageTextEvaluation first')
    for path in sets:
        score(path, '-v' in sys.argv)


main()
