"""通过内置协议及临时注册插件检查异步路由；不直接插入任务数据库。"""
import hashlib
import json
import time
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager

from .common import require
from .routing_cases import wait_received

FIXTURE_MODEL = 'routing-origin'


def register_origin_plugin(e):
    key = 'routing-origin-' + e.suffix
    e.origin_path = '/routing-origin-' + e.suffix + '/jobs'
    meta = {'apiVersion':1, 'key':key, 'name':'Routing reference contract', 'version':'1.0.0',
        'author':{'name':'Local tests'}, 'models':[FIXTURE_MODEL], 'fetchMode':'per_task',
        'routes':[{'method':'POST','path':e.origin_path,'type':'submit','decode':'create','render':'created'}]}
    source = 'export const meta = '+json.dumps(meta)+';\n'+r'''
export const native = {
 create(ctx) { const r=ctx.body.value; return {kind:"submit",model:"routing-origin",action:"text_to_video",
   originTaskIds:r.origin ? [r.origin] : [], requestBody:r}; },
 created(ctx,task) { return {task_id:task.task_id}; }
};
export function buildSubmitRequest(ctx) {
 const origin=(ctx.originTasks||[])[0];
 return {url:ctx.baseUrl+"/v1/videos"+(origin?"/"+origin.upstreamTaskId+"/remix":""),method:"POST",
   headers:{Authorization:"Bearer "+ctx.apiKey},body:{model:"sora-2",prompt:"routing reference",seconds:"4",size:"1280x720"}};
}
export function parseSubmitResponse(ctx,response) { return {taskId:response.body.id,taskData:response.body}; }
export function buildQueryRequest(ctx) { return {url:ctx.baseUrl+"/v1/videos/"+ctx.taskId,method:"GET",headers:{Authorization:"Bearer "+ctx.apiKey}}; }
export function parseTaskResult(ctx,body) { return {taskId:ctx.taskId,status:body.status==="completed"?"SUCCESS":body.status==="failed"?"FAILURE":"IN_PROGRESS",progress:body.status==="completed"?"100%":"50%"}; }
'''
    e.root.api('POST','/api/plugin/task',{'source':source,'enabled':True,
        'sourceSha256':hashlib.sha256(source.encode()).hexdigest(),'remark':'isolated origin route contract'})
    return key


def public_id(raw):
    value=json.loads(raw)
    task=value.get('data',value)
    identifier=task.get('task_id') or task.get('id') or task.get('result')
    require(isinstance(identifier,str) and bool(identifier),'task_public_id_missing')
    return identifier


def task_path(identifier, mj):
    return '/mj/task/'+identifier+'/fetch' if mj else '/v1/tasks/'+identifier


def wait_tasks(e, identifiers, mj=False):
    """Poll every pending task together so one native poll pass can finish them all."""
    deadline=time.monotonic()+90
    pending=list(identifiers)
    while pending:
        for identifier in list(pending):
            status,raw=e.user.request('GET',task_path(identifier,mj),bearer=e.key)
            require(status==200,'task_poll_status_'+str(status))
            value=json.loads(raw)
            task=value.get('data',value)
            if task.get('status') in ('SUCCESS','success','completed'):
                pending.remove(identifier)
                continue
            require(task.get('status') not in ('FAILURE','failed'),'task_lifecycle_failed')
        if not pending:
            return
        require(time.monotonic()<deadline,'task_lifecycle_timeout')
        time.sleep(.2)


def wait_task(e, identifier, mj=False):
    wait_tasks(e,[identifier],mj)


def submit_pending(e,path,body,amount,channel):
    """Submit one task and check its charge and single supplier POST without waiting for completion."""
    before,cursor=e.balances(),e.sim.cursor()
    status,raw=e.request(path,body,expected_charge=amount)
    require(status==200,'task_submit_status_'+str(status))
    identifier=public_id(raw)
    after=e.settled(before,amount)
    e.zero()
    posts=[row for row in e.sim.records(cursor) if row['method']=='POST' and '/submit/' in row['path'] or
           row['method']=='POST' and row['path'] in ('/v1/videos','/v1/videos/{task_id}/remix','/v1/video_generation','/v2/video_generation')]
    require(len(posts)==1 and posts[0]['channel']==channel and posts[0]['auth_valid'], 'task_wrong_submission_count_or_channel')
    e.exchanges[-1].update(expected_charge=amount,actual_charge=before['wallet']-after['wallet'],
                           supplier_sequences=[row['sequence'] for row in posts],task_id=identifier)
    return identifier


def finish_tasks(e,before,identifiers,amount,mj=False):
    """Completion must not move the wallet away from the sum charged at submit."""
    wait_tasks(e,identifiers,mj)
    e.settled(before,amount)
    e.zero()


def submit(e,path,body,amount,channel,*,mj=False):
    before=e.balances()
    identifier=submit_pending(e,path,body,amount,channel)
    finish_tasks(e,before,[identifier],amount,mj)
    return identifier


def lifecycle(e,plugin,model,body,amount):
    e.route([plugin])
    identifier=submit(e,'/v1/tasks/'+plugin,{'model':model,'prompt':'lifecycle',**body},amount,plugin)
    return {'task_id':identifier,'terminal':'SUCCESS','expected_charge':amount}


@contextmanager
def secondary_token(e):
    original=(e.token_id,e.key)
    name='cross-key-'+e.suffix+'-'+str(len(e.tokens))
    e.user.api('POST','/api/token/',{'name':name,'remain_quota':1000000,'expired_time':-1,
                                   'model_limits_enabled':False})
    token=next(row for row in e.user.api('GET','/api/token/?p=1&page_size=100')['items'] if row['name']==name)
    e.token_id=token['id']
    e.tokens.append(e.token_id)
    e.key=e.user.api('POST',f'/api/token/{e.token_id}/key')['key']
    try:
        yield
    finally:
        e.token_id,e.key=original


def plugin_origin(e):
    e.route(['origin'])
    original=submit(e,e.origin_path,{'model':FIXTURE_MODEL},1000,'origin')
    with secondary_token(e):
        e.route(['b'])
        submit(e,e.origin_path,{'model':FIXTURE_MODEL,'origin':original},1000,'origin')
    # Same-token continuations only depend on the finished origin and billing is
    # persisted at submit, so they finish together before the channel is disabled.
    batch_before,pending=e.balances(),[]
    e.route(['b'])
    pending.append(submit_pending(e,e.origin_path,{'model':FIXTURE_MODEL,'origin':original},1000,'origin'))
    e.configure('origin',channel_ratio='2')
    e.route(['b'],cap='1')
    raw,_=e.check([],status=400,amount=0,path=e.origin_path,body={'model':FIXTURE_MODEL,'origin':original})
    require(json.loads(raw).get('code')=='channel_ratio_exceeded','origin_price_error_code_changed')
    e.route(['b'],cap='2')
    pending.append(submit_pending(e,e.origin_path,{'model':FIXTURE_MODEL,'origin':original},2000,'origin'))
    finish_tasks(e,batch_before,pending,3000)
    e.route(['b'])
    e.status('origin',2)
    e.check([],status=400,amount=0,path=e.origin_path,body={'model':FIXTURE_MODEL,'origin':original})
    e.status('origin',1)
    e.configure('origin',max_concurrency=1,channel_ratio='1')
    e.sim.scenario('origin',steps=[{'kind':'hold'},{'kind':'success'}],task_states=['queued','completed'])
    cursor,before=e.sim.cursor(),e.balances()
    with ThreadPoolExecutor(max_workers=1) as pool:
        held=pool.submit(e.request,e.origin_path,{'model':FIXTURE_MODEL,'origin':original})
        try:
            wait_received(e,cursor,'origin')
            status,_=e.request(e.origin_path,{'model':FIXTURE_MODEL,'origin':original},expected_charge=0)
            require(status==429,'origin_full_not_rejected')
        finally:
            e.sim.release('origin')
        status,raw=held.result(timeout=10)
        require(status==200,'held_origin_submit_failed')
    wait_task(e,public_id(raw))
    e.settled(before,1000)
    e.zero()
    return {'fixture':'native API registered plugin using Sora wire protocol', 'origin_selected_list_exempt':True,
            'price_cap_enforced':True,'exact_cap_allowed':True,'full_rejected':True}


def midjourney(e):
    e.route(['mj'])
    origin=submit(e,'/mj/submit/imagine',{'prompt':'a lighthouse'},1000,'mj',mj=True)
    with secondary_token(e):
        e.route(['b'])
        submit(e,'/mj/submit/change',{'taskId':origin,'action':'UPSCALE','index':1},1000,'mj',mj=True)
    batch_before,pending=e.balances(),[]
    e.route(['b'])
    operations=[('change',{'action':'UPSCALE','index':1}),('modal',{'prompt':'add a moon'}),
                ('video',{'prompt':'pan left'}),('action',{'customId':'MJ::JOB::upsample::1::fixture'})]
    for operation,body in operations:
        pending.append(submit_pending(e,'/mj/submit/'+operation,{'taskId':origin,**body},1000,'mj'))
    e.configure('mj',channel_ratio='2')
    e.route(['b'],cap='1')
    raw,_=e.check([],status=400,amount=0,path='/mj/submit/change',body={'taskId':origin,'action':'UPSCALE','index':1})
    require(json.loads(raw)['error']['code']=='channel_ratio_exceeded','mj_price_error_code_changed')
    raw,_=e.check([],status=400,amount=0,path='/mj/task/'+origin+'/image-seed',method='GET')
    require(json.loads(raw).get('code')==4,'mj_seed_error_wrapper_changed')
    e.route(['b'],cap='2')
    pending.append(submit_pending(e,'/mj/submit/change',{'taskId':origin,'action':'UPSCALE','index':1},2000,'mj'))
    e.check(['mj'],amount=0,path='/mj/task/'+origin+'/image-seed',method='GET')
    finish_tasks(e,batch_before,pending,6000,mj=True)
    e.status('mj',2)
    e.check([],status=400,amount=0,path='/mj/submit/change',body={'taskId':origin,'action':'UPSCALE','index':1})
    e.status('mj',1)
    e.configure('mj',channel_ratio='1',max_concurrency=1)
    e.sim.scenario('mj',steps=[{'kind':'hold'},{'kind':'success'}],task_states=['queued','completed'])
    before,cursor=e.balances(),e.sim.cursor()
    with ThreadPoolExecutor(max_workers=1) as pool:
        held=pool.submit(e.request,'/mj/submit/change',{'taskId':origin,'action':'UPSCALE','index':1})
        try:
            wait_received(e,cursor,'mj')
            require(e.request('/mj/submit/change',{'taskId':origin,'action':'UPSCALE','index':1},expected_charge=0)[0]==429,
                    'mj_full_not_rejected')
        finally:
            e.sim.release('mj')
        status,raw=held.result(timeout=10)
        require(status==200,'mj_held_submit_failed')
    wait_task(e,public_id(raw),mj=True)
    e.settled(before,1000)
    e.zero()
    return {'origin_task':origin,'operations':[name for name,_ in operations],'price_cap_enforced':True}


CASES=[('sora_task_lifecycle',lambda e:lifecycle(e,'sora','sora-2',{'seconds':'4','size':'1280x720'},4000)),
       ('hailuo_task_lifecycle',lambda e:lifecycle(e,'hailuo','MiniMax-Hailuo-02',{'duration':6,'resolution':'768P'},6000)),
       ('plugin_origin_pin_price_and_capacity',plugin_origin),('mj_continuation_pin_price',midjourney)]
