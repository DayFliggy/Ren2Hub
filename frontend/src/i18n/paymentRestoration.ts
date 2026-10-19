import { nativePaymentMessages } from './nativePayment'

export const paymentRestorationMessages = {
  'zh-CN': {
    ...nativePaymentMessages['zh-CN'],
    wallet: {
      quickTopup: '快捷充值',
      amountPlaceholder: '输入金额',
      summaryBalanceAfter: '到账后余额',
      callbackNote: '服务端核验支付成功后，额度将自动到账。',
      topupNow: '立即充值',
      quoteLoading: '正在更新支付报价…',
      retryQuote: '重新获取报价',
      invalidTopupAmount: '请输入符合最低数量和充值步长的安全整数。',
      gmpaySubmit: '前往收银台支付',
      gmpayRedirectNote:
        '将跳转到支付网关收银台，请在那里完成转账。到账以服务端核验的回调为准，跳转本身不代表支付成功。',
      gmpayNetworkGuidance:
        '请使用所选代币付款，并在转账前核对收银台显示的网络、地址和金额。',
    },
    systemSettings: {
      gmpayPanel: {
        title: 'GMPay 虚拟币支付',
        address: '网关地址',
        merchantId: '商户 ID',
        secret: '商户密钥',
        configured: '已配置',
        keepSecret: '留空会保留现有密钥。',
        enableNewOrders: '开放新订单',
        enableNewOrdersHint:
          '关闭后停止创建 GMPay 新订单。已有订单仍会使用下单时保存的商户凭据核验付款通知，核验通过后入账。',
        probeHint:
          '探测只读取公开资产目录，不创建订单，也不会校验商户凭据。凭据在创建支付订单时校验。',
        callbackHint:
          '通知地址由通用支付的回调基础地址加 /api/user/gmpay/notify 生成；留空基础地址时使用站点服务器地址。网关资产、钱包和链监听在 epusdt 自身管理，探测不会修改网关。',
        probe: '探测可用资产',
        save: '保存网关配置',
        remove: '移除资产',
        removeAsset: '移除 {asset}',
        assetName: '{asset} 显示名',
        assetIcon: '{asset} 图标标识（可选）',
        assetColor: '{asset} 颜色元数据（可选）',
        metadataHint:
          '保留给支付配置 API 的展示元数据；当前钱包不使用自定义图标或颜色。',
        addressRequired: '请先填写网关地址。',
        noAssets: '暂无可用支付资产，请检查网关已启用的网络和钱包。',
        probed: '已读取可用资产，请选择要开放的代币。',
        saved: 'GMPay 配置已保存',
        loading: '正在读取网关配置…',
        retryLoad: '重新读取网关配置',
      },
    },
    paymentRestoration: {
      credit: '到账额度',
      noSelectedAssets:
        '尚未选择支付资产。请加载可用资产并选择要向用户开放的币种和网络。',
      creditNote: '到账额度与充值后余额以订单完成后的实际记录为准。',
      currencyNote: '支付币种以收银台显示为准。',
    },
  },
  en: {
    ...nativePaymentMessages.en,
    wallet: {
      quickTopup: 'Quick top-up',
      amountPlaceholder: 'Enter amount',
      summaryBalanceAfter: 'Balance after',
      callbackNote:
        'Credit arrival is confirmed by the server after verifying payment.',
      topupNow: 'Top up now',
      quoteLoading: 'Updating payment quote…',
      retryQuote: 'Retry quote',
      invalidTopupAmount:
        'Enter a safe integer that meets the minimum quantity and recharge step.',
      gmpaySubmit: 'Continue to cashier',
      gmpayRedirectNote:
        'You will leave this site for the payment gateway cashier and complete the transfer there. Credit arrival is confirmed by the server callback; the redirect itself does not prove payment.',
      gmpayNetworkGuidance:
        'Pay with the selected token. Before transferring, check the network, address, and amount shown in the cashier.',
    },
    systemSettings: {
      gmpayPanel: {
        title: 'GMPay crypto payments',
        address: 'Gateway address',
        merchantId: 'Merchant ID',
        secret: 'Merchant secret',
        configured: 'Configured',
        keepSecret: 'Leave blank to keep the existing secret.',
        enableNewOrders: 'Accept new orders',
        enableNewOrdersHint:
          'Disabling this stops new GMPay orders. Valid payment notifications for existing orders still credit the wallet using the merchant credentials saved with each order.',
        probeHint:
          'The probe reads the public asset catalog without creating an order or checking merchant credentials. Credentials are checked when a payment order is created.',
        callbackHint:
          'The notification URL appends /api/user/gmpay/notify to the shared callback base URL, falling back to the site server address. Assets, wallets, and chain monitoring are managed in epusdt itself; probing does not change the gateway.',
        probe: 'Load available assets',
        save: 'Save gateway configuration',
        remove: 'Remove asset',
        removeAsset: 'Remove {asset}',
        assetName: '{asset} display name',
        assetIcon: '{asset} icon identifier (optional)',
        assetColor: '{asset} color metadata (optional)',
        metadataHint:
          'Display metadata for the payment configuration API. The current wallet does not use custom icons or colors.',
        addressRequired: 'Enter the gateway address first.',
        noAssets:
          'No payment assets are available. Check the enabled networks and wallets on the gateway.',
        probed: 'Available assets loaded. Select the tokens to offer.',
        saved: 'GMPay configuration saved',
        loading: 'Loading gateway configuration…',
        retryLoad: 'Reload gateway configuration',
      },
    },
    paymentRestoration: {
      credit: 'Credit received',
      noSelectedAssets:
        'No assets selected. Load the asset catalog and choose the tokens and networks to offer.',
      creditNote:
        'Credited quota and the updated balance are available after the order completes.',
      currencyNote: 'Confirm the payment currency at checkout.',
    },
  },
}
