#!/usr/bin/env python3
"""Drive zig cc for Go's external static link with the pinned musl Cronet."""
import os
import pathlib
import sys

zig = os.environ["OPENWEIR_ZIG"]
target = os.environ["OPENWEIR_MUSL_TARGET"]
args = sys.argv[1:]
if "-l:libcronet.a" in args:
    # Zig reads the colon form as a dynamic -l argument. Go supplies the
    # verified module directory as -L in this same compiler invocation.
    archives = [pathlib.Path(a[2:]) / "libcronet.a" for a in args if a.startswith("-L")]
    archives = [p for p in archives if p.is_file()]
    if len(archives) != 1:
        sys.exit("could not identify a single Cronet archive")
    args = [str(archives[0]) if a == "-l:libcronet.a" else a for a in args]
# These are empty compatibility archives under musl; the symbols live in libc.
args = [a for a in args if a not in ("-lresolv", "-lpthread", "-lrt", "-ldl")]
os.execv(zig, [zig, "cc", "-target", target, *args])
