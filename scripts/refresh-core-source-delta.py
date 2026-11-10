#!/usr/bin/env python3
"""更新冻结核心的文件差异；不将工作树哈希或历史测试当作候选身份。"""
import argparse
import collections
import hashlib
import json
from pathlib import Path
import subprocess
import sys

# 本轮已审计的 NewAPI 核心版本；清单的 upstream_sha 与 reference_newapi_sha 都取这里。
UPSTREAM_SHA = '48a6c87f9e7757fdc58e5be9bcb4f8281ad0dfd0'


def git(*args, repository=None):
    return subprocess.check_output(['git', *args], cwd=repository or ROOT)


def requires_reasons(row):
    # 新增和删除也是本地偏离，不能靠文件分类绕过合同依据检查。
    path = row['path']
    return (row['status'] in ('adapted', 'extension', 'removed') and not path.endswith('_test.go')
            and '/testdata/' not in path and not path.startswith('testdata/'))


ROOT = Path(__file__).resolve().parent.parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--upstream-repo', type=Path, required=True,
                    help='只读参考仓库，必须已有清单中的冻结核心对象；不 fetch')
args = parser.parse_args()
MANIFEST = ROOT / 'docs/upstream/CORE_SOURCE_DELTA.json'
data = json.loads(MANIFEST.read_text())
upstream = UPSTREAM_SHA
previous = {row['path'] for row in data['files']}
reasons = {row['path']: row['reasons'] for row in data['files'] if row.get('reasons')}
reviews = {row['path']: row['review'] for row in data['files'] if row.get('review')}
contracts = data.get('contracts', {})
directories = sorted({path.split('/')[0] for path in previous if '/' in path})
root_files = sorted({path for path in previous if '/' not in path}
                    | {path.name for path in ROOT.glob('*.go')})
scopes = directories + root_files
if any(path == 'docs' or path.startswith('docs/') or path in ('.', '..') for path in scopes):
    raise ValueError('documentation is outside the core manifest scope')
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
    row = dict(path=path, status=status, upstream_sha256=source, local_sha256=local)
    # 回到上游一致的文件不再需要理由，旧理由随之丢弃。
    if status != 'upstream_equal' and path in reasons:
        row['reasons'] = reasons[path]
    if status != 'upstream_equal' and path in reviews:
        row['review'] = reviews[path]
    rows.append(row)
missing = [row['path'] for row in rows if requires_reasons(row) and (
    not row.get('reasons') or not row.get('review')
    or any(reason not in contracts for reason in row.get('reasons', []))
    or row.get('review', {}).get('target') != upstream
    or not row.get('review', {}).get('basis'))]
if missing:
    print('production differences without reviewed contract reasons (manifest not written):', file=sys.stderr)
    for path in missing:
        print('  ' + path, file=sys.stderr)
    sys.exit(1)
data.update(upstream_sha=upstream,
            implementation_base_sha='a276b48eaa60ae2ff1e761c21f5ac5fb526a541c',
            history_baseline_sha='9a63062227c31ba54a6346e07ee8b4fd8802f22f',
            reference_newapi_sha=upstream,
            candidate_sha=None, counts=dict(collections.Counter(row['status'] for row in rows)), files=rows)
data.pop('audit_repair_base_sha', None)
MANIFEST.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({'upstream_sha': upstream, 'candidate_sha': None, 'counts': data['counts']}))
