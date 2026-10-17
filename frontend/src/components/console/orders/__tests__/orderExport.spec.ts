import { expect, it } from 'vitest'
import { serializeOrders } from '../orderExport'
import type { TopupOrder } from '@/api/topup'
it('exports documented native fields and protects spreadsheet formulas', () => {
  const order = {
    id: 7,
    user_id: 9,
    trade_no: '=SUM(1)',
    amount: 10,
    money: 1.25,
    payment_provider: 'gmpay',
    payment_method: 'usdt.tron',
    create_time: 100,
    complete_time: 0,
    status: 'pending',
    secret: 'never-export',
  } as TopupOrder
  const [csv] = serializeOrders([order], 'csv')
  expect(csv).toContain("'=SUM(1)")
  expect(csv).not.toContain('never-export')
  const [json] = serializeOrders([order], 'json')
  expect(JSON.parse(json)).toEqual([
    {
      id: 7,
      user_id: 9,
      trade_no: '=SUM(1)',
      amount: 10,
      money: 1.25,
      payment_provider: 'gmpay',
      payment_method: 'usdt.tron',
      create_time: 100,
      complete_time: 0,
      status: 'pending',
    },
  ])
})
