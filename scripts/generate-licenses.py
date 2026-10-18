"""Regenerate direct Go / Vue licenses from installed, locked dependencies."""
import argparse,hashlib,json,pathlib,re,subprocess
root=pathlib.Path(__file__).resolve().parents[1]
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--evidence-dir',type=pathlib.Path)
args=parser.parse_args()
evidence=args.evidence_dir
if evidence: evidence.mkdir(parents=True,exist_ok=True)
rows=json.loads((root/'scripts/license-inventory.json').read_text())
raw=subprocess.check_output(['go','list','-m','-json','all'],cwd=root,text=True)
dec=json.JSONDecoder(); modules={}
while raw.strip():
 raw=raw.lstrip(); obj,n=dec.raw_decode(raw); raw=raw[n:]; modules[obj['Path']]=obj
expected=set()
for folder in ['.','relaykit','tokenkit']:
 m=json.loads(subprocess.check_output(['go','mod','edit','-json'],cwd=root/folder))
 expected.update(r['Path'] for r in m['Require'] if not r.get('Indirect',False) and not r['Path'].startswith('github.com/QuantumNous/new-api/'))
assert expected=={r['name'] for r in rows if r['ecosystem']=='Go'}
for folder in ['frontend']:
 m=json.loads((root/folder/'package.json').read_text())
 assert set(m.get('dependencies',{}))|set(m.get('devDependencies',{}))=={r['name'] for r in rows if r['area']==folder}
texts={}; notices=[]; files=[]
for row in rows:
 name=row['name']; go=row['ecosystem']=='Go'
 directory=pathlib.Path(modules[name]['Dir']) if go else root/row['area']/'node_modules'/name
 row['version']=modules[name]['Version'] if go else json.loads((directory/'package.json').read_text())['version']
 if name=='lucide-vue-next': row['license']='ISC; Feather portions: MIT'
 if name=='marked': row['license']='MIT; Markdown portions: BSD-3-Clause'
 if name=='echarts': row['license']='Apache-2.0; d3 portions: BSD-3-Clause'
 paths=[p for p in sorted(directory.iterdir()) if p.is_file() and re.match(r'^(licen[sc]e|copying|notice|unlicense|copyright)',p.name,re.I)]
 for folder in ['licenses','license','LICENSES']:
  if (directory/folder).is_dir(): paths.extend(p for p in sorted((directory/folder).rglob('*')) if p.is_file())
 row['text_refs']=[]
 for path in paths:
  original=path.read_bytes(); text=original.decode('utf-8-sig'); text='\n'.join(line.rstrip() for line in text.splitlines()).strip()+'\n'
  file=path.relative_to(directory).as_posix(); source=f'{name} {row["version"]} — {file}'
  files.append(dict(package=name,version=row['version'],file=file,sha256=hashlib.sha256(original).hexdigest()))
  if path.name.lower().startswith('notice'):
   notices.append((source,text)); continue
  digest=hashlib.sha256(text.encode()).hexdigest()
  if digest not in texts: texts[digest]=dict(number=len(texts)+1,text=text,sources=[])
  entry=texts[digest];entry['sources'].append(source)
  if entry['number'] not in row['text_refs']:row['text_refs'].append(entry['number'])
 assert row['text_refs'],name
header='''# Third-Party Licenses

This file records the direct third-party dependencies of Ren2Hub and their supplied license texts. Keep it with `LICENSE` and `NOTICE` in Docker images, standalone binary distributions, and frontend distributions.

Inventory checked on 2026-10-18 against direct `require` entries in `go.mod`, `relaykit/go.mod` and `tokenkit/go.mod`, using versions resolved by the root Go module; Vue versions come from `frontend/bun.lock`, verified against package metadata after frozen-lockfile installation. Go license identifiers were checked against the module-cache license files. Local RelayKit and TokenKit replacements are first-party source and are excluded. Development/test dependencies are identified separately.

The inventory covers direct dependencies, including additional license files supplied alongside their top-level license. It is not a complete inventory of the transitive dependency graph, operating-system packages, or browser/runtime components. Preserve notices shipped by those components. Independent asset notices are outside this direct-package inventory and must be retained with their assets. No React dependency inventory is used.

Full upstream license texts below preserve their copyright statements and bundled-component terms; only line endings and trailing whitespace are normalized for this document. Identical texts are included once and linked from every applicable row. Upstream `NOTICE` files are reproduced in `NOTICE`; they supplement these licenses.

## MPL-2.0 source availability

The unmodified `github.com/go-sql-driver/mysql v1.7.0` module is distributed under MPL-2.0. Its complete license, including the exhibits, is reproduced in its linked text below. Source for the exact dependency version is available from [the upstream v1.7.0 tag](https://github.com/go-sql-driver/mysql/tree/v1.7.0) and [the Go module source archive](https://proxy.golang.org/github.com/go-sql-driver/mysql/@v/v1.7.0.zip). `go mod download github.com/go-sql-driver/mysql@v1.7.0` retrieves that source version.

MPL-2.0 sections 3.1–3.4 require distributors of its executable form to make the corresponding covered source, including any modifications, available under MPL-2.0 and tell recipients how to obtain it; they also require preserving license notices and prohibit restricting recipients' rights to that source. If this dependency is modified or replaced, update the source-availability information to the actual distributed version and make those covered modifications available. Including a license identifier or this inventory alone does not fulfill the source-availability requirement.

## Dependency inventory

| Area | Scope | Ecosystem | Dependency | Version | License | Supplied text |
| --- | --- | --- | --- | --- | --- | --- |
'''
lines=[header]
for row in rows:
 refs=', '.join(f'[{n}](#license-{n})' for n in row['text_refs'])
 lines.append(f'| {row["area"]} | {row["scope"]} | {row["ecosystem"]} | `{row["name"]}` | `{row["version"]}` | {row["license"]} | {refs} |\n')
lines.append('\n## Supplied license texts\n')
for entry in texts.values():
 lines.append(f'\n### License {entry["number"]}\n\n')
 for source in entry['sources']:lines.append(f'- `{source}`\n')
 fence='`'*(max([len(m.group(0)) for m in re.finditer(r'`+',entry['text'])]+[2])+1)
 lines.append('\n'+fence+'text\n'+entry['text']+fence+'\n')
(root/'THIRD-PARTY-LICENSES.md').write_text(''.join(lines))
notice=(root/'NOTICE').read_text().split('==== Third-Party Notices ====')[0]
notice+='''==== Third-Party Notices ====

This product includes third-party open source software. The exact direct
versions, copyright statements and supplied license texts are recorded in
THIRD-PARTY-LICENSES.md. Preserve that file and this NOTICE with distributions.
The upstream NOTICE texts below include development/test dependencies listed
in that inventory; their presence does not mean those tools ship at runtime.

'''
for source,text in notices:notice+='==== '+source+' ====\n\n'+text+'\n'
notice+='''==== MPL-2.0 source availability ====

This product includes the unmodified github.com/go-sql-driver/mysql v1.7.0.
Its MPL-2.0 license is reproduced in THIRD-PARTY-LICENSES.md. Corresponding
source is available at:
https://github.com/go-sql-driver/mysql/tree/v1.7.0
https://proxy.golang.org/github.com/go-sql-driver/mysql/@v/v1.7.0.zip

Distributors must preserve the MPL notices and make the corresponding
covered source, including any covered modifications, available under
MPL-2.0 as described in sections 3.1-3.4 of that license.

==== End of Notices ====
'''
(root/'NOTICE').write_text(notice)
if evidence:
 (evidence/'license-source-files.json').write_text(json.dumps(files,indent=2)+'\n')
 (evidence/'license-inventory.json').write_text(json.dumps(rows,indent=2)+'\n')
print(f'{len(rows)} dependencies; {len(files)} license/notice files; {len(texts)} unique license texts; {len(notices)} NOTICE files; {(root/"THIRD-PARTY-LICENSES.md").stat().st_size} bytes')
