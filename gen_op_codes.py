import argparse
import os
import subprocess
import sys


def proc(f):
    if 'GOPACKAGE' in os.environ:
        yield 'package %s' % os.environ['GOPACKAGE']
        yield ''

    for line in f:
        if line.startswith('var ops = '):
            break

    yield 'const ('

    code = 0

    for line in f:
        if line.startswith('}'):
            break
        line = line.strip()
        if line.startswith('//'):
            continue
        for part in line.split(','):
            part = part.strip()
            if not part or part.startswith('//'):
                continue

            name = ''
            i = part.find('"') + 1
            if i > 0:
                j = part.find('"', i)
                if j >= 0:
                    name = part[i:j]

            if name:
                yield 'opCode%s = opCode(0x%02x)' % (name.title(), code)

            code += 1

    yield ')'


parser = argparse.ArgumentParser()
parser.add_argument(
    '-i', metavar='file', type=argparse.FileType('r'), default=sys.stdin)
parser.add_argument(
    '-o', metavar='file', type=argparse.FileType('w'), default=sys.stdout)
args = parser.parse_args()

gofmt = subprocess.Popen('gofmt', stdin=subprocess.PIPE, stdout=args.o)

for line in proc(args.i):
    print >>gofmt.stdin, line

gofmt.stdin.close()
assert gofmt.wait() == 0
