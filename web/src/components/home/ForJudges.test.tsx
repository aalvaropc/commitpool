import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { embeddableVideo } from '../../lib/demoInfo'
import ForJudges, { DemoVideo, SandboxBuyer } from './ForJudges'

const VIDEO = 'https://www.youtube-nocookie.com/embed/aB3_x-9Zk0Q'
const LOGIN = { email: 'buyer@example.com', password: 'sandbox-secret-1' }

describe('For judges', () => {
  it('draws no login and no video when the server sends neither', () => {
    for (const info of [undefined, null, { workshops: true }]) {
      const html = renderToStaticMarkup(<ForJudges info={info} />)
      expect(html).not.toContain('id="buyer-title"')
      expect(html).not.toContain('Copy')
      expect(html).not.toContain('<iframe')
      expect(html).toContain('The PayPal Sandbox buyer login for step 3 is in the testing instructions on Devpost.')
      expect(html).not.toContain('Sandbox buyer below')
    }
  })

  it('shows the Sandbox buyer with copy buttons and keeps the password off the page', () => {
    const html = renderToStaticMarkup(<ForJudges info={{ workshops: true, judge_login: LOGIN }} />)
    expect(html).toContain('<h3 id="buyer-title" class="text-base">PayPal Sandbox buyer</h3>')
    expect(html).toContain('buyer@example.com')
    expect(html).toContain('aria-label="Copy the Sandbox buyer email"')
    expect(html).toContain('aria-label="Copy the Sandbox buyer password"')
    expect(html).toContain('Sandbox buyer below')
    expect(html).not.toContain(LOGIN.password)
    expect(html).not.toContain('testing instructions on Devpost')
  })

  it('embeds the video privately, lazily and with a referrer YouTube accepts', () => {
    const html = renderToStaticMarkup(<DemoVideo url={VIDEO} />)
    expect(html).toContain(`src="${VIDEO}"`)
    expect(html).toContain('loading="lazy"')
    expect(html).toContain('referrerPolicy="strict-origin-when-cross-origin"')
    expect(html).toMatch(/title="CommitPool demo video[^"]+"/)
  })

  it('refuses anything but a youtube-nocookie embed of one video', () => {
    expect(embeddableVideo(VIDEO)).toBe(VIDEO)
    for (const url of [undefined, '', 'https://www.youtube.com/embed/aB3_x-9Zk0Q', 'https://www.youtube-nocookie.com/embed/aB3_x-9Zk0Q?autoplay=1', 'https://www.youtube-nocookie.com/embed/', 'javascript:alert(1)']) {
      expect(embeddableVideo(url)).toBeNull()
      expect(renderToStaticMarkup(<DemoVideo url={url} />)).toBe('')
    }
  })

  it('tells real from prepared from accelerated', () => {
    const html = renderToStaticMarkup(<ForJudges info={null} />)
    for (const text of ['Real Sandbox calls', 'Claude on Amazon Bedrock', 'Prepared', 'Accelerated clock', 'Logged, not sent', '“Skip to the last 2 hours”']) {
      expect(html).toContain(text)
    }
  })

  it('renders no buyer box without a login', () => {
    expect(renderToStaticMarkup(<SandboxBuyer login={undefined} />)).not.toContain('Copy')
  })
})
