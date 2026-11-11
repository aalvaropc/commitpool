import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { embeddableVideo } from '../../lib/demoInfo'
import ForJudges, { DemoVideo, SandboxBuyer, VideoFrame } from './ForJudges'

const VIDEO = 'https://www.youtube-nocookie.com/embed/aB3_x-9Zk0Q'
const LOGIN = { email: 'buyer@example.com', password: 'sandbox-secret-1' }

describe('For judges', () => {
  it('draws no login and no video when the server sends neither', () => {
    for (const info of [undefined, null, { workshops: true }]) {
      const html = renderToStaticMarkup(<ForJudges info={info} />)
      expect(html).not.toContain('id="buyer-title"')
      expect(html).not.toContain('Copy')
      expect(html).not.toContain('<iframe')
      expect(html).toContain('The PayPal Sandbox buyer login for the 4-minute path is in the testing instructions on Devpost.')
      expect(html).not.toContain('Sandbox buyer below')
    }
  })

  it('lists the 30-second path first and links the repo', () => {
    const html = renderToStaticMarkup(<ForJudges info={null} />)
    const quick = html.indexOf('30 seconds')
    const long = html.indexOf('About 4 minutes')
    expect(quick).toBeGreaterThan(-1)
    expect(long).toBeGreaterThan(quick)
    expect(html).toContain('type an answer in the chat above.')
    expect(html).toContain('Try it as Iris, read her group chat, then 3 PayPal checkouts. PayPal remembers your login after the first.')
    expect(html).toContain('href="https://github.com/aalvaropc/commitpool"')
    expect(html).toContain('Code on GitHub')
    expect(html).not.toContain('Devpost</a>')
    expect(html).not.toContain('Three steps')
  })

  it('links Devpost only when the server sends it and only over https', () => {
    const link = 'https://devpost.com/software/commitpool'
    expect(renderToStaticMarkup(<ForJudges info={{ workshops: true, devpost_url: link }} />)).toContain(`href="${link}"`)
    for (const bad of ['http://devpost.com/x', 'javascript:alert(1)', '']) {
      expect(renderToStaticMarkup(<ForJudges info={{ workshops: true, devpost_url: bad }} />)).not.toContain('Devpost</a>')
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

  it('shows the video’s poster and loads nothing from YouTube until it is played', () => {
    const html = renderToStaticMarkup(<DemoVideo url={VIDEO} />)
    expect(html).toContain('aria-label="Watch the demo video"')
    expect(html).toContain('src="https://i.ytimg.com/vi/aB3_x-9Zk0Q/hqdefault.jpg"')
    expect(html).not.toContain('<iframe')
  })

  it('embeds the video privately, lazily and with a referrer YouTube accepts', () => {
    const html = renderToStaticMarkup(<VideoFrame src={VIDEO} autoplay />)
    expect(html).toContain(`src="${VIDEO}?autoplay=1"`)
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
    for (const text of ['Real Sandbox calls', 'Claude on Amazon Bedrock', 'Prepared', 'Accelerated clock', 'Logged, not sent', '“Skip to the last 2 hours”', '“Fail next charge”', 'Simulated failure', 'A PayPal Sandbox mock response']) {
      expect(html).toContain(text)
    }
  })

  it('renders no buyer box without a login', () => {
    expect(renderToStaticMarkup(<SandboxBuyer login={undefined} />)).not.toContain('Copy')
  })
})
