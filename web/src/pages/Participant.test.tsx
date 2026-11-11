import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { FromTheChat } from './Participant'

const text = (html: string) => html.replace(/<[^>]+>/g, ' ').replaceAll('&#x27;', "'").replaceAll('&amp;', '&').replace(/\s+/g, ' ')

describe('the participant page of someone added from the chat', () => {
  it('quotes what they said and asks them to say it again if it is wrong', () => {
    const plain = text(renderToStaticMarkup(<FromTheChat host="Iris Calder" message="El sábado no puedo, pero el domingo me viene perfecto." canAnswer />))
    expect(plain).toContain('What you said in the chat')
    expect(plain).toContain('Iris added this from the group chat:')
    expect(plain).toContain('El sábado no puedo, pero el domingo me viene perfecto.')
    expect(plain).toContain("If it’s wrong, say it again in your own words.")
  })

  it('does not ask for a new answer once answers are closed', () => {
    expect(text(renderToStaticMarkup(<FromTheChat host="Iris Calder" message="Sunday works." canAnswer={false} />))).not.toContain('say it again')
  })
})
