export const clientConfigMessages = {
  'zh-CN': {
    title: '客户端配置',
    hint: '配置引用 REN2HUB_API_KEY 环境变量，请在客户端进程中设置密钥；配置不含完整密钥。',
    client: '客户端',
    model: '模型',
    server_address: '客户端服务地址',
    server_address_hint:
      '填写客户端可访问的地址并保留反向代理路径前缀；仅用于本次导出，未以 /v1 结尾时自动补上。',
    current_site: '使用当前站点地址',
    loading: '正在核对配置…',
    retry: '重新核对',
    generate: '生成配置',
    copy: '复制配置',
    copied: '已复制',
    download: '下载',
    close: '关闭',
    preview: '配置预览',
    prices_omitted:
      '价格由网关结算，配置不填写价格。Pi 的客户端成本统计可能显示 0，不代表免费。',
    codex_limits: '接口未提供上下文和输出上限，配置不声明这些值，请自行核对。',
    pi_limits: '接口未提供以下能力，请按供应商资料填写，仅用于本次配置。',
    context_window: '上下文上限（Token）',
    max_tokens: '最大输出（Token）',
    reasoning: '推理能力',
    input: '输入类型',
    choose: '请选择',
    yes: '支持',
    no: '不支持',
    text: '文本',
    image: '文本与图片',
    codex_install: '将内容合并到 ~/.codex/config.toml，保留已有配置。',
    pi_install:
      '将 providers.ren2hub 合并到 ~/.pi/agent/models.json，然后在 Pi 中运行 /model 选择模型。',
    route_note: '导出前会核对此 Key 的模型权限和协议路由，不保证实时容量。',
    error: {
      invalid_address:
        '请填写有效的 HTTP 或 HTTPS 服务地址，不含用户名、密码、查询参数或片段。',
      no_models: '此 Key 暂无可用的模型候选。请检查模型权限和渠道配置。',
      no_protocol_models:
        '此 Key 的模型候选中没有明确支持所选客户端协议的模型，请切换客户端或核对渠道和模型端点。',
      model_unavailable:
        '此模型当前没有支持所选客户端协议的授权路由。请选择其他模型或检查路由配置。',
      token_unavailable: '此 Key 已停用、过期或额度耗尽，请先恢复其可用状态。',
      pi_settings_required:
        '请填写正整数上下文和输出上限（输出不能超过上下文），并明确选择推理及输入能力。',
      failed: '无法生成配置，请重试。',
      copy_failed: '复制失败，请手动复制预览内容或下载文件。',
    },
  },
  en: {
    title: 'Client configuration',
    hint: 'The configuration references the REN2HUB_API_KEY environment variable; set the key in the client process. It never contains the full key.',
    client: 'Client',
    model: 'Model',
    server_address: 'Client server address',
    server_address_hint:
      'Use an address your client can reach, keeping any reverse proxy path prefix. Applies to this export only; /v1 is appended unless the address ends with it.',
    current_site: 'Use current site address',
    loading: 'Checking configuration…',
    retry: 'Check again',
    generate: 'Generate configuration',
    copy: 'Copy configuration',
    copied: 'Copied',
    download: 'Download',
    close: 'Close',
    preview: 'Configuration preview',
    prices_omitted:
      'The gateway settles charges; prices are omitted. Pi may show zero client cost, which does not mean free usage.',
    codex_limits:
      'The API does not provide context or output limits, so the configuration omits them. Check them yourself.',
    pi_limits:
      'The API does not provide these values. Enter them from your provider; they apply to this configuration only.',
    context_window: 'Context window (tokens)',
    max_tokens: 'Maximum output (tokens)',
    reasoning: 'Reasoning support',
    input: 'Input types',
    choose: 'Select an option',
    yes: 'Supported',
    no: 'Not supported',
    text: 'Text',
    image: 'Text and images',
    codex_install:
      'Merge this into ~/.codex/config.toml, preserving your existing settings.',
    pi_install:
      'Merge providers.ren2hub into ~/.pi/agent/models.json, then run /model in Pi to select the model.',
    route_note:
      'Model access and protocol routes for this Key are checked before export. Live capacity is not guaranteed.',
    error: {
      invalid_address:
        'Enter a valid HTTP or HTTPS server address without a username, password, query or fragment.',
      no_models:
        'This Key has no model candidates. Check its model permissions and channels.',
      no_protocol_models:
        'No model candidate explicitly supports this client protocol. Switch clients or check channel and model endpoints.',
      model_unavailable:
        'This model has no authorized route for the selected client protocol. Choose another model or check routing settings.',
      token_unavailable:
        'This Key is disabled, expired, or exhausted. Restore its active state first.',
      pi_settings_required:
        'Enter positive integer context and output limits (output must not exceed context), and select reasoning and input capabilities.',
      failed: 'Unable to generate configuration. Please try again.',
      copy_failed:
        'Copy failed. Copy the preview manually or download the file.',
    },
  },
}
