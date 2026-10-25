"""Build panel schemas from a public method list and server docs (URL or file).

Only GET /docs/swagger.json is performed; no business endpoint is called.
"""
import argparse
import hashlib
import json
from pathlib import Path
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
FORBIDDEN = {'wxid', 'xwxid', 'authorization', 'accesstoken', 'refreshtoken', 'licensetoken'}

def normalize(key):
    return key.replace('_', '').replace('-', '').lower()

HELP = {
    'base64': '图片文件的纯 Base64，不带 data: 前缀。',
    'toWxid': '接收人微信 ID 或群 ID。',
    'fileAesKey': '图片 CDN 解密密钥，来自收到的媒体消息或上传结果；不是登录 token。',
    'fileNo': '图片 CDN 文件编号，来自媒体消息或上传结果。',
    'v3': '好友请求/搜索结果中的 v3，直接使用该业务返回值。',
    'v4': '好友请求/搜索结果中的 v4，与同一次请求的 v3 配套。',
    'scene': '好友操作的来源场景值，使用业务事件/搜索结果对应值；文档未列出完整枚举。',
    'friend_flag': '好友标志值，可省略；遵循当前好友验证场景，文档未列出枚举。',
    'friendFlag': '好友标志值，使用当前验证场景对应值；文档未列出枚举。',
    'verifyContent': '发送好友申请时的验证文字。',
    'remarks': '给目标好友设置的备注文字。',
    'modOperation': '好友权限修改操作值；服务文档未列出枚举，请使用业务协议要求的值。',
    'modType': '好友权限修改类型；服务文档未列出枚举。',
    'modTypeInfoList': '权限修改项列表；结构见展开的嵌套参数文档。',
    'permKind': '权限种类标志；服务文档未列出枚举。',
    'clientMsgId': '待撤回消息的客户端消息 ID，来自原消息/发送结果。',
    'newMsgId': '待撤回消息的服务消息 ID，来自原消息/发送结果；保留完整整数。',
    'createTime': '原消息的创建时间，直接使用原消息/发送结果中的值。',
    'xml': '要分享的视频消息 XML，按服务端要求构造或取自原始消息。',
    'sendOriginal': '是否发送原图，使用 JSON 布尔值 true / false。',
    'aesKey': 'CDN 上传返回的文件 AES 密钥，按原值传入。',
    'clientImgId': '图片客户端标识，按 CDN 上传结果/协议传入。',
    'crc32': '图片 CRC32 校验值，使用 CDN 上传结果中的值。',
    'fileId': 'CDN 上传返回的文件 ID。',
    'imgFileName': '图片文件名，按上传结果/协议传入。',
    'md5': '图片内容的 MD5 校验值，使用上传结果中的值。',
    'midSize': '中图大小，按 CDN 上传结果传入。',
    'thumbH': '缩略图高度，按 CDN 上传结果传入。',
    'thumbW': '缩略图宽度，按 CDN 上传结果传入。',
    'thumbSize': '缩略图大小，按 CDN 上传结果传入。',
    'tp_username': '要查询的企业微信联系人标识。',
    'ticket': '联系人查询的业务 ticket；使用消息/联系人业务返回的值，不是宿主连接 token。',
    'wording_id_list': '待查询的资源文案 ID 数组。',
    'app_id': '目标应用/公众号 ID，具体格式以所选业务接口为准。',
    'language': '资源语言标识，格式以服务端业务协议为准。',
}

def sample(schema, name=''):
    if 'example' in schema:
        return schema['example']
    if 'default' in schema:
        return schema['default']
    ty = schema.get('type')
    if isinstance(ty, list): ty = next(t for t in ty if t != 'null')
    if ty == 'object': return {k: sample(v, k) for k, v in schema.get('properties', {}).items()}
    if ty == 'array': return [sample(schema.get('items', {}), name)]
    if ty == 'boolean': return False
    if ty in ('integer', 'number'):
        return {'voiceTime': 1000, 'playLength': 2}.get(name, 0)
    if ty == 'string':
        if 'base64' in name.lower() or name.endswith('b64'): return 'AA=='
        if name in ('chatRoomId',): return 'example@chatroom'
        if name in ('toWxid', 'toWxids'): return 'wxid_example'
        if name in ('url', 'thumbUrl'): return 'https://example.com/image.jpg'
        if name == 'app_id': return 'gh_example'
        return ''
    return None


def build(allowed_methods, spec, source):
    def resolve(schema, seen=()):
        if '$ref' in schema:
            ref = schema['$ref'].rsplit('/', 1)[-1]
            if ref in seen: return {'type': 'object', 'description': '递归对象；参见服务文档。'}
            return resolve(spec['definitions'][ref], (*seen, ref))
        result = {k: ({n: resolve(v, seen) for n, v in val.items()} if k == 'properties'
                    else resolve(val, seen) if k == 'items' else val) for k, val in schema.items()}
        if 'properties' in result: result['additionalProperties'] = False
        return result
    methods = []
    names = set()
    for entry in allowed_methods:
        name, path = entry['name'], entry['path']
        if name in names:
            raise ValueError(f'Duplicate public method: {name}')
        names.add(name)
        operation = spec.get('paths', {}).get(path, {}).get('post')
        if operation is None:
            raise ValueError(f'Missing POST documentation for {name}: {path}')
        body = next((p for p in operation.get('parameters', []) if p['in'] == 'body'), {})
        schema = resolve(body.get('schema', {'type': 'object', 'properties': {}}))
        props = {k: v for k, v in schema.get('properties', {}).items() if normalize(k) not in FORBIDDEN}
        required = [k for k in schema.get('required', []) if k in props]
        sending = name in {'msg.send_text_message','msg.send_app_message','msg.send_share_link','msg.send_video_message','msg.send_cdn_file','msg.send_cdn_img','msg.send_cdn_video','msg.send_emoji','msg.send_quote','msg.send_voice','msg.share_card','msg.share_location','msg.share_video','msg.send_image_via_cdn','msg.send_cdn_img_after_upload'}
        if sending and 'toWxid' not in required: required.append('toWxid')
        if name == 'msg.send_text_message': required.append('content')
        fields=[]
        for key, field in props.items():
            media_help = {
                ('msg.send_voice','base64'): '已编码语音文件的纯 Base64，不带 data: 前缀；宿主不自动转码。',
                ('msg.send_voice','type'): '语音编码格式编号，必须与 Base64 内容一致；当前文档未列出完整枚举。',
                ('msg.send_voice','voiceTime'): '语音时长，示例 1000；当前文档未注明单位，按服务端约定传入。',
                ('msg.send_video_message','base64'): '视频文件的纯 Base64，不带 data: 前缀。',
                ('msg.send_video_message','imageBase64'): '视频封面图片的纯 Base64。',
                ('msg.send_video_message','playLength'): '视频时长；按当前服务的时长约定传入，示例为 2。',
            }.get((name,key))
            if media_help and not field.get('description'): field['description']=media_help
            field['description'] = field.get('description') or HELP.get(key) or f'服务端字段 {key}，语义以接口说明为准。'
            fields.append({'name':key,'required':key in required,'schema':field,'description':field['description']})
        input_schema={'type':'object','properties':props,'required':required,'additionalProperties':False}
        example=sample(input_schema)
        if name == 'msg.send_text_message': example['content']='hello from plugin'
        methods.append({'name':name,'group':name.split('.')[0],'path':path,
            'title':operation.get('summary',name),'description':operation.get('description',''),
            'body_description':body.get('description',''),'sending':sending,
            'fields':fields,'schema':input_schema,'example':example,
            'response_description':operation.get('responses',{}).get('200',{}).get('description',''),
            'response_schema':resolve(operation.get('responses',{}).get('200',{}).get('schema',{})),
            'return_note':'统一 CallResult：call_id / status / data / raw / messages / error；所有调用记录日志，发送消息入库。超时仅停止等待，请用 call_id 查询；不自动重发。'})
    return {'version':2,'source':source,'source_sha256':hashlib.sha256(json.dumps(spec,sort_keys=True).encode()).hexdigest(),
            'methods':methods,'counts':{'business':len(methods),'total':len(methods)}}

if __name__ == '__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--methods-file',type=Path,default=ROOT/'ws/python/wechat_panel/catalog.json',
                        help='Public JSON catalog containing a methods list with name/path entries')
    parser.add_argument('--output',type=Path,default=ROOT/'ws/python/wechat_panel/catalog.json')
    sources=parser.add_mutually_exclusive_group()
    sources.add_argument('--docs-url',default='http://localhost:8081/docs/swagger.json')
    sources.add_argument('--docs-file',type=Path,help='Current server docs/swagger.json, including a WSL UNC path')
    args=parser.parse_args()
    if args.docs_file:
        spec=json.loads(args.docs_file.read_text(encoding='utf-8'))
        source=args.docs_file.name
    else:
        with urllib.request.urlopen(args.docs_url,timeout=10) as response: spec=json.load(response)
        source=args.docs_url
    methods=json.loads(args.methods_file.read_text(encoding='utf-8'))['methods']
    catalog=build(methods,spec,source)
    args.output.write_text(json.dumps(catalog,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    print(catalog['counts'])
