#!/usr/bin/env python3
"""更新冻结核心的文件差异；不将工作树哈希或历史测试当作候选身份。"""
import argparse
import collections
import difflib
import hashlib
import json
from pathlib import Path
import subprocess
import sys

# 本轮已审计的 NewAPI 核心版本；清单的 upstream_sha 与 reference_newapi_sha 都取这里。
UPSTREAM_SHA = 'b388140acfd80369ec6243b16869daff1b4051f4'
TEST_ASSETS = {'router/frontend/embed-dist/probe.txt': 'router/native_restored_web_test.go',
               'router/frontend/embed-dist/brotli-probe.js': 'router/web_brotli_test.go',
               'router/frontend/embed-dist/brotli-probe.js.br': 'router/web_brotli_test.go'}


def git(*args, repository=None):
    return subprocess.check_output(['git', *args], cwd=repository or ROOT)


def requires_reasons(row):
    # 新增和删除也是本地偏离，不能靠文件分类绕过合同依据检查。
    path = row['path']
    return (row['status'] in ('adapted', 'extension', 'removed') and path not in TEST_ASSETS and not path.endswith('_test.go')
            and '/testdata/' not in path and not path.startswith('testdata/'))


ROOT = Path(__file__).resolve().parent.parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--upstream-repo', type=Path,
                    help='只读参考仓库，必须已有清单中的冻结核心对象；不 fetch')
parser.add_argument('--check-local', action='store_true',
                    help='只核对清单记录的本地哈希和文件覆盖，不读取上游，不写清单')
args = parser.parse_args()
if not args.check_local and args.upstream_repo is None:
    parser.error('--upstream-repo is required unless --check-local is given')
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
local_paths = set(git('ls-files', '--cached', '--others', '--exclude-standard', '-z', '--', *scopes)
                  .decode().strip('\0').split('\0'))
# 原始 1109 段属于基线审查；对后来重新逐段审查的文件，刷新文件哈希前须先核对本次裁定。
if any(review.get('hunk_review') == 'NATIVE_B388_REVIEW.json' for review in reviews.values()):
    review_data = json.loads((MANIFEST.parent / 'NATIVE_B388_REVIEW.json').read_text())
    current = review_data.get('current_file_reviews', {})
    expected = {path for path, review in reviews.items() if review.get('current_hunk_review')}
    if expected != current.keys():
        sys.exit('current hunk review file mismatch: ' + ', '.join(sorted(expected ^ current.keys())))
    for path, review in current.items():
        local_file = ROOT / path
        local = local_file.read_bytes() if local_file.is_file() else None
        if local is None or hashlib.sha256(local).hexdigest() != review['local_sha256']:
            sys.exit(f'{path}: current hunk review needs updating')
        if not args.check_local:
            source = git('show', upstream + ':' + path, repository=args.upstream_repo)
            diff = difflib.diff_bytes(difflib.unified_diff, source.splitlines(True), local.splitlines(True))
            hunks = []
            for line in list(diff)[2:]:
                if line.startswith(b'@@ '):
                    hunks.append(hashlib.sha256())
                hunks[-1].update(line)
            if [h.hexdigest() for h in hunks] != [h['sha256'] for h in review['hunks']]:
                sys.exit(f'{path}: reviewed hunks differ from frozen upstream diff')
if args.check_local:
    # 上游侧哈希只有刷新时才能复算；这里只回答“工作树是否已偏离清单”。
    recorded = {row['path']: row['local_sha256'] for row in data['files']}
    stale = []
    for path in sorted((previous | local_paths) - {''}):
        local_file = ROOT / path
        local = hashlib.sha256(local_file.read_bytes()).hexdigest() if local_file.is_file() else None
        if recorded.get(path) != local:
            stale.append(path)
    for path in stale:
        print(path, file=sys.stderr)
    print(json.dumps({'checked': len(recorded), 'stale': len(stale)}))
    sys.exit(1 if stale else 0)
source_paths = set(git('ls-tree', '-r', '--name-only', '-z', upstream, '--', *scopes, repository=args.upstream_repo)
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
    if path in TEST_ASSETS:
        row['test_fixture_for'] = TEST_ASSETS[path]
    # 回到上游一致的文件不再需要理由，旧理由随之丢弃。
    if status != 'upstream_equal' and path in reasons:
        row['reasons'] = reasons[path]
    if status != 'upstream_equal' and path in reviews:
        row['review'] = reviews[path]
    rows.append(row)
missing = [row['path'] for row in rows if requires_reasons(row) and (
    not row.get('reasons') or not row.get('review')
    or any(reason not in contracts or contracts[reason].get('kind') != 'approved_contract'
           for reason in row.get('reasons', []))
    or row.get('review', {}).get('target') != upstream
    or not row.get('review', {}).get('basis'))]
if missing:
    print('production differences without reviewed contract reasons (manifest not written):', file=sys.stderr)
    for path in missing:
        print('  ' + path, file=sys.stderr)
    sys.exit(1)
data.update(upstream_sha=upstream,
            implementation_base_sha='2382fbdd55a6d76d73d8e5b314c7765c98cd8da6',
            history_baseline_sha='9a63062227c31ba54a6346e07ee8b4fd8802f22f',
            reference_newapi_sha=upstream,
            candidate_sha=None, counts=dict(collections.Counter(row['status'] for row in rows)), files=rows)
data.pop('audit_repair_base_sha', None)
MANIFEST.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({'upstream_sha': upstream, 'candidate_sha': None, 'counts': data['counts']}))
