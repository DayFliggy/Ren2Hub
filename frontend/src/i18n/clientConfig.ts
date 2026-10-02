export const clientConfigMessages = {
  'zh-CN': {
    title: '客户端配置',
    hint: '配置只引用 REN2HUB_API_KEY 环境变量。请在客户端进程中设置密钥；预览和下载不会修改客户端配置。',
    client: '客户端',
    model: '模型',
    loading: '正在生成配置…',
    retry: '重试',
    copy: '复制配置',
    copied: '已复制',
    download: '下载',
    close: '关闭',
    preview: '配置预览',
    diagnostics: '导出说明',
    prices_omitted: '价格由网关结算；配置未填写客户端价格。',
    codex_capabilities_partial:
      'Codex 配置只携带可映射的上下文上限，其他能力未自动声明。',
    pi_input_modality_omitted: 'Pi 配置未声明不支持映射的输入模态。',
    pi_input_unknown: '输入模态无法映射，已省略；请核查客户端默认值。',
    pi_capabilities_partial: '工具和输出模态无法完整映射，已省略。',
  },
  en: {
    title: 'Client configuration',
    hint: 'Configuration references the REN2HUB_API_KEY environment variable. Set the key in the client process. Previewing and downloading do not modify client settings.',
    client: 'Client',
    model: 'Model',
    loading: 'Generating configuration…',
    retry: 'Retry',
    copy: 'Copy configuration',
    copied: 'Copied',
    download: 'Download',
    close: 'Close',
    preview: 'Configuration preview',
    diagnostics: 'Export notes',
    prices_omitted: 'The gateway settles charges. Client prices are omitted.',
    codex_capabilities_partial:
      'Only the known context limit maps to this Codex configuration; other capabilities are omitted.',
    pi_input_modality_omitted:
      'Input modalities without a Pi mapping are omitted.',
    pi_input_unknown:
      'Input modalities cannot be mapped and are omitted. Check client defaults.',
    pi_capabilities_partial:
      'Tool and output capabilities cannot be fully mapped and are omitted.',
  },
}
