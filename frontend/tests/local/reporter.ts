import { writeFileSync } from 'node:fs'
import type {
  FullResult,
  Reporter,
  TestCase,
  TestResult,
} from '@playwright/test/reporter'

export default class LocalReporter implements Reporter {
  private tests: Array<{
    title: string
    status: string
    duration_ms: number
    steps: Array<{ title: string; success: boolean }>
  }> = []

  onTestEnd(test: TestCase, result: TestResult) {
    if (result.error)
      console.error(
        result.error.message?.replace(
          /eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/g,
          '[redacted token]'
        )
      )
    this.tests.push({
      title: test.title,
      status: result.status,
      duration_ms: result.duration,
      steps: result.steps
        .filter((step) => step.category === 'test.step')
        .map((step) => ({ title: step.title, success: !step.error })),
    })
  }

  onEnd(result: FullResult) {
    const passed = this.tests.filter((test) => test.status === 'passed').length
    const skipped = this.tests.filter(
      (test) => test.status === 'skipped'
    ).length
    writeFileSync(
      process.env.REN2HUB_LOCAL_OUTPUT + '/results.json',
      JSON.stringify(
        {
          status: result.status,
          passed,
          skipped,
          failed: this.tests.length - passed - skipped,
          tests: this.tests,
        },
        null,
        2
      ),
      { mode: 0o600, flag: 'wx' }
    )
  }
}
