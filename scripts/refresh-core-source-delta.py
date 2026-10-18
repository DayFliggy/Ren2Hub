#!/usr/bin/env python3
"""更新冻结核心的文件差异；不将工作树哈希或历史测试当作候选身份。"""
import argparse
import collections
import hashlib
import json
from pathlib import Path
import subprocess


def git(*args, repository=None):
    return subprocess.check_output(['git', *args], cwd=repository or ROOT)


ROOT = Path(__file__).resolve().parent.parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--upstream-repo', type=Path, required=True,
                    help='只读参考仓库，必须已有清单中的冻结核心对象；不 fetch')
args = parser.parse_args()
MANIFEST = ROOT / 'docs/upstream/CORE_SOURCE_DELTA.json'
data = json.loads(MANIFEST.read_text())
upstream = data['upstream_sha']
previous = {row['path'] for row in data['files']}
directories = sorted({path.split('/')[0] for path in previous if '/' in path})
root_files = sorted({path for path in previous if '/' not in path}
                    | {path.name for path in ROOT.glob('*.go')})
scopes = directories + root_files
source_paths = set(git('ls-tree', '-r', '--name-only', '-z', upstream, '--', *scopes, repository=args.upstream_repo)
                   .decode().strip('\0').split('\0'))
local_paths = set(git('ls-files', '--cached', '--others', '--exclude-standard', '-z', '--', *scopes)
                  .decode().strip('\0').split('\0'))
rows = []
for path in sorted((previous | source_paths | local_paths) - {''}):
    if path.startswith('docs/developer-notes/'):
        raise ValueError('human-only path outside core manifest scope')
    source = hashlib.sha256(git('show', upstream + ':' + path, repository=args.upstream_repo)).hexdigest() if path in source_paths else None
    local_file = ROOT / path
    local = hashlib.sha256(local_file.read_bytes()).hexdigest() if local_file.is_file() else None
    if source is None and local is None:
        continue
    status = ('extension' if source is None else 'removed' if local is None
              else 'upstream_equal' if source == local else 'adapted')
    rows.append(dict(path=path, status=status, upstream_sha256=source, local_sha256=local))
data.update(implementation_base_sha='39daa6ccee738f7a26fdba131c829be1adbbdd5b',
            history_baseline_sha='9a63062227c31ba54a6346e07ee8b4fd8802f22f',
            reference_newapi_sha='6cf0d90b5ff27f901697dddd61e0c28aba8efefc',
            candidate_sha=None, counts=dict(collections.Counter(row['status'] for row in rows)), files=rows)
data.pop('audit_repair_base_sha', None)
MANIFEST.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({'upstream_sha': upstream, 'candidate_sha': None, 'counts': data['counts']}))
