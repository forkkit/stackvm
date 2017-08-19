import argparse
import re
import sys
from itertools import tee
from subprocess import check_call
from tempfile import NamedTemporaryFile


def wrap(s, prefixes=('// - ', '//   ')):
    k = 0
    s = prefixes[k] + s
    while s:
        i = s.rfind(' ', len(prefixes[k])+1, 78) if len(s) > 78 else -1
        if i <= 0:
            yield s
            break
        s, r = s[:i], s[i+1:]
        yield s
        if not r:
            break
        if k < len(prefixes)-1:
            k += 1
        s = prefixes[k] + r


docsPat = re.compile(r'\s*// (.+)')
camelWord = re.compile('.+?(?:(?<=[a-z])(?=[A-Z])|(?<=[A-Z])(?=[A-Z][a-z])|$)')


def extract(lines, each):
    docs = ''
    for line in lines:
        m = each.match(line)
        if m:
            name, value = m.groups()
            name = ' '.join(
                m.group(0).lower()
                for m in camelWord.finditer(name))
            for s in wrap('%s %s: %s' % (value, name, docs)):
                yield s
            docs = ''
            continue
        m = docsPat.match(line)
        if m:
            if docs:
                docs += ' ' + m.group(1)
            else:
                docs = m.group(1)
        else:
            docs = ''


parser = argparse.ArgumentParser()
parser.add_argument('-i', metavar='file')
parser.add_argument('-o', metavar='file')
parser.add_argument('each', metavar='prefix')
parser.add_argument('target', metavar='pattern')
parser.add_argument('sep', metavar='pattern')
args = parser.parse_args()

each = re.compile(r'\s*%s(\w+)\s*(?:\w+\s*)?=\s*([^\s]*)' % args.each)
target = re.compile(args.target)
sep = re.compile(args.sep)

with NamedTemporaryFile() as tmp:
    with open(args.i, 'r') as fi:
        ia, ib = tee(fi)
        state = 0
        for line in ib:
            if state == 0 and target.search(line):
                state = 1
                tmp.write(line)
                for line in extract(ia, each):
                    tmp.write(line + '\n')
                continue
            if state == 1:
                if sep.search(line):
                    state = 2
                    tmp.write(line)
                continue
            tmp.write(line)

    with open(args.o, 'w') as fo:
        tmp.flush()
        check_call(['gofmt', tmp.name], stdout=fo)
