#!/usr/bin/env python3
"""从当前工作树不可变快照构建审计镜像；不提交、不发布。"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

from candidate import (execute, private_write, require, source_identity,
                       inspect_build_payload, validate_build_identity)

SOURCE = Path(__file__).resolve().parents[2]


def build(source, output):
    before = source_identity(source)
    snapshot = output / 'source'
    snapshot.mkdir()
    names = execute(['git','-C',str(source),'ls-files','-z','--cached','--others','--exclude-standard',
                     '--','.',':(exclude)docs/developer-notes',':(exclude)**/developer-notes/**']).stdout.split(b'\0')
    for raw in sorted(set(names)-{b''}):
        name = Path(os.fsdecode(raw))
        require('developer-notes' not in name.parts and not name.is_absolute() and '..' not in name.parts,
                'unsafe_snapshot_path')
        origin, destination = source/name, snapshot/name
        if not origin.exists() and not origin.is_symlink():
            continue
        # Submodule gitlinks are not build files. This project resolves its Go modules as normal directories.
        if origin.is_dir():
            continue
        destination.parent.mkdir(parents=True,exist_ok=True)
        if origin.is_symlink():
            target = os.readlink(origin)
            require(not Path(target).is_absolute(), 'absolute_build_symlink')
            require((origin.parent/target).resolve().is_relative_to(source.resolve()),'external_build_symlink')
            destination.symlink_to(target)
        else:
            shutil.copy2(origin,destination)
    require(source_identity(source)==before,'source_changed_while_snapshotting')
    # Compare every copied byte; Docker never reads the live tree.
    for raw in sorted(set(names)-{b''}):
        name=Path(os.fsdecode(raw))
        if (source/name).is_file() and not (source/name).is_symlink():
            require(hashlib.sha256((source/name).read_bytes()).digest()==hashlib.sha256((snapshot/name).read_bytes()).digest(),
                    'snapshot_content_mismatch')
    tag='ren2hub-routing:'+before['working_tree_sha256'][:16]
    docker=['sudo','-n','docker','--host','unix:///var/run/docker.sock']
    command=docker+['build','--quiet',
        '--build-arg','APP_VERSION='+before['git_head'],
        '--label','org.opencontainers.image.revision='+before['git_head'],
        '--label','org.opencontainers.image.version='+before['git_head'],
        '--label','io.ren2hub.source-sha256='+before['working_tree_sha256'],str(snapshot)]
    require(not (output/'identity.json').exists(), 'new_build_identity_required')
    report={'success':False,'source_before':before,'build_command':command}
    private_write(output/'build-report.json',report)
    try:
        with (output/'build.log').open('wb') as stream:
            built = subprocess.run(command,stdout=subprocess.PIPE,stderr=stream,check=True,timeout=1800)
        image_id=built.stdout.decode().strip()
        require(re.fullmatch(r'sha256:[0-9a-f]{64}',image_id),'immutable_image_id_required')
        image=json.loads(execute(docker+['image','inspect',image_id]).stdout)[0]
        payload=inspect_build_payload(lambda *args,**kwargs:execute(docker+list(args),**kwargs),image_id,'ren2hub')
        tools={**payload['tools'], 'python':sys.version.split()[0],
               'docker_server':execute(docker+['version','--format','{{.Server.Version}}']).stdout.decode().strip(),
               'docker_client':execute(docker+['version','--format','{{.Client.Version}}']).stdout.decode().strip()}
        after=source_identity(source)
        manifest={'schema_version':1,'artifact':'ren2hub','success':True,'stable':before==after,
                  'image_id':image_id,'tag':tag,'source_before':before,'source_after':after,
                  'revision':before['git_head'],'base_revision':before['git_head'],
                  'binary':payload['binary'],'tools':tools}
        validate_build_identity(manifest,image,'ren2hub',after,payload)
        require(tools['go']=='go1.26.10' and tools['bun']=='1.4.0' and tools['node'].startswith('v24.'),
                'build_tool_version_mismatch')
        execute(docker+['tag',image_id,tag])
        require(source_identity(source)==before,'source_changed_before_manifest')
    finally:
        report['source_after']=source_identity(source)
        report['stable']=report['source_after']==before
        private_write(output/'build-report.json',report)
    require(report['stable'], 'source_changed_before_manifest')
    private_write(output/'identity.json',manifest)
    report.update(success=True,image_id=image_id,identity_sha256=hashlib.sha256((output/'identity.json').read_bytes()).hexdigest())
    private_write(output/'build-report.json',report)
    return manifest


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-parent',type=Path,default=SOURCE/'.local-tests')
    args=parser.parse_args()
    os.umask(0o077)
    args.output_parent.mkdir(parents=True,exist_ok=True)
    output=Path(tempfile.mkdtemp(prefix='routing-image-',dir=args.output_parent)).resolve()
    print('构建证据：'+str(output),flush=True)
    report=build(SOURCE,output)
    print(json.dumps({key:report[key] for key in ('image_id','source_before','stable')},ensure_ascii=False))


if __name__=='__main__':
    main()
