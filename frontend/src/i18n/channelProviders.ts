export const channelProviderMessages = {
  'zh-CN': {
    awsApiType: 'AWS 接口',
    awsKeyType: 'AWS 凭证类型',
    awsAccessKey: 'Access Key / Secret Access Key',
    invokeModel: 'InvokeModel（默认）',
    mantle: 'Mantle Messages',
    awsHint:
      'Mantle 支持 Claude Messages、流式输出和顶层缓存控制。Amazon Nova 使用 InvokeModel。',
    awsCredentials:
      '凭证格式：APIKey{separator}Region 或 AccessKey{separator}SecretAccessKey{separator}Region。',
    vertexKeyType: 'Vertex 凭证类型',
    serviceAccount: '服务账号 JSON',
    apiKey: 'API Key',
    projectId: 'Vertex 项目 ID（可选）',
    projectHint:
      '标准 Vertex API 填写项目 ID；Express 模式留空。服务账号使用 JSON 中的项目。',
    region: 'Vertex 区域',
    regionHint:
      '例如 global、us、eu 或 us-central1，也可填写按模型映射的 JSON。',
    regionRequired: '请填写 Vertex 区域。',
  },
  en: {
    awsApiType: 'AWS API',
    awsKeyType: 'AWS credential type',
    awsAccessKey: 'Access Key / Secret Access Key',
    invokeModel: 'InvokeModel (default)',
    mantle: 'Mantle Messages',
    awsHint:
      'Mantle supports Claude Messages, streaming and top-level cache control. Use InvokeModel for Amazon Nova.',
    awsCredentials:
      'Credentials: APIKey{separator}Region or AccessKey{separator}SecretAccessKey{separator}Region.',
    vertexKeyType: 'Vertex credential type',
    serviceAccount: 'Service account JSON',
    apiKey: 'API Key',
    projectId: 'Vertex project ID (optional)',
    projectHint:
      'Enter the project for the standard Vertex API; leave empty for express mode. Service accounts use the project in their JSON.',
    region: 'Vertex region',
    regionHint:
      'For example global, us, eu or us-central1, or a per-model JSON map.',
    regionRequired: 'Enter a Vertex region.',
  },
}
