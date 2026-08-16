"""Report the generated files whose tests cover less than the target.

Two things about the measurement matter enough to be code rather than a comment
in a shell script:

  * it reads a profile produced with -coverpkg, because a mapper called from
    another package reads as 0% in a plain per-package profile;
  * it merges blocks with MAX, not by summing, because with -coverpkg every
    test binary reports every package, and summing counts each statement once
    per binary — inflating the denominator by the number of packages.

Getting either wrong changes the answer by roughly an order of magnitude, in
opposite directions, which is how a coverage number ends up meaning nothing.
"""

import collections
import sys

# These need a database or a running HTTP app to execute at all. The boot lane
# of the golden gate starts the service for real and calls it, which is a truer
# test of them than a unit test against a fake app would be — that would test
# the web framework.
EXEMPT = ("_repository.go", "_service.go", "_service_manual.go", "_routes.go")

TARGET = 80.0


def main(path):
    blocks = {}
    with open(path) as fh:
        for line in fh:
            if line.startswith("mode:"):
                continue
            loc, count, hits = line.rsplit(" ", 2)
            filename, span = loc.split(":", 1)
            statements, covered = int(count), int(hits) > 0
            key = (filename, span)
            blocks[key] = (statements, blocks.get(key, (statements, False))[1] or covered)

    per_file = collections.defaultdict(lambda: [0, 0])
    for (filename, _), (statements, covered) in blocks.items():
        per_file[filename][0] += statements
        if covered:
            per_file[filename][1] += statements

    short = []
    for filename, (statements, covered) in sorted(per_file.items()):
        if "/internal/" not in filename or not statements:
            continue
        if filename.endswith(EXEMPT):
            continue
        pct = 100.0 * covered / statements
        if pct < TARGET:
            short.append("%s at %.0f%%" % (filename.split("/internal/")[-1], pct))

    print("; ".join(short))


if __name__ == "__main__":
    main(sys.argv[1])
