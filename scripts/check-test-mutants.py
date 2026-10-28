#!/usr/bin/env python3
"""Verify regression tests reject deliberate mutants in isolated copies.
Never mutates the working tree or reads real clipboard data.
"""
import pathlib, shutil, subprocess, tempfile, sys

root = pathlib.Path(__file__).resolve().parents[1]
cases = [
    ('trailing-document', 'storage.go', 'if err := decoder.Decode(&extra); err != io.EOF {', 'if err := decoder.Decode(&extra); false && err != io.EOF {', ['go','test','-run','^TestStorageRejectsTrailingDocuments$','-count=1']),
    ('case-sensitive-search', 'search.go', 'q = strings.ToLower(strings.TrimSpace(q))', 'q = strings.TrimSpace(q)', ['go','test','-run','^TestCachedQueryMatchesLiteralBaseline$','-count=1']),
    ('postrename-hidden', 'storage.go', 'return &replacedWriteError{err}', 'return nil', ['go','test','-run','^TestStorageWriteFailureBoundaries$','-count=1']),
    ('wrong-target', 'native_darwin.go', 'frontPID != targetPID ||', 'false ||', ['go','test','-run','^TestNativePasteInputGuard$','-count=1']),
    ('stale-detail-cache', 'web/responsiveness.js', 'this.generation++; this.items.clear();', 'this.generation++;', ['node','--test','web/responsiveness.test.mjs']),
]
failed = False
for name, filename, old, new, command in cases:
    with tempfile.TemporaryDirectory(prefix='clipnest-mutant-') as temp:
        target = pathlib.Path(temp)
        for path in root.glob('*.go'): shutil.copy2(path, target/path.name)
        for filename2 in ('go.mod','go.sum'): shutil.copy2(root/filename2,target/filename2)
        shutil.copytree(root/'web',target/'web')
        path=target/filename
        text=path.read_text()
        if text.count(old)!=1: raise RuntimeError(f'{name}: mutation anchor not unique')
        path.write_text(text.replace(old,new))
        result=subprocess.run(command,cwd=target,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
        behavior_failure = result.returncode != 0 and ('--- FAIL:' in result.stdout or 'not ok ' in result.stdout) and 'build failed' not in result.stdout
        print(name, 'REJECTED' if behavior_failure else 'INVALID', 'exit',result.returncode)
        if not behavior_failure: print(result.stdout);failed=True
sys.exit(1 if failed else 0)
