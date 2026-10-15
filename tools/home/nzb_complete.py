"""Home fork: how complete is an NZB? For each <file> whose subject says how
many parts it was posted in ("(1/4029)"), count the parts the NZB lists.
Offline: reads only the NZB files, nothing goes to Usenet.

    python nzb_complete.py <folder or .nzb>...
"""
import os, re, sys
import xml.etree.ElementTree as ET

NS = '{http://www.newzbin.com/DTD/2003/nzb}'
PARTS = re.compile(r'\((\d+)/(\d+)\)\s*$')


def missing(path):
    """(missing parts, total parts, files short) for one NZB, video and rar files only."""
    root = ET.parse(path).getroot()
    miss = total = short = 0
    for f in root.findall(NS + 'file'):
        subj = f.get('subject', '')
        low = subj.lower()
        if '.par2' in low or '.nfo' in low or '.jpg' in low or '.sfv' in low or '.txt' in low:
            continue
        m = PARTS.search(subj)
        if not m:
            continue
        want = int(m.group(2))
        have = len({s.get('number') for s in f.iter(NS + 'segment')})
        total += want
        if have < want:
            miss += want - have
            short += 1
    return miss, total, short


def main(args):
    paths = []
    for a in args:
        if os.path.isdir(a):
            paths += [os.path.join(a, n) for n in sorted(os.listdir(a)) if n.lower().endswith('.nzb')]
        else:
            paths.append(a)
    bad = 0
    for p in paths:
        try:
            m, t, s = missing(p)
        except Exception as e:
            print(f'{os.path.basename(p)}: unreadable ({e})')
            continue
        if m:
            bad += 1
            print(f'{os.path.basename(p)}: {m} of {t} parts missing ({s} files short)')
    print(f'{bad} of {len(paths)} NZBs are incomplete')


if __name__ == '__main__':
    main(sys.argv[1:])
