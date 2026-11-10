import AgentFeed from '../components/home/AgentFeed'
import Closing from '../components/home/Closing'
import Faq from '../components/home/Faq'
import ForJudges from '../components/home/ForJudges'
import Hero from '../components/home/Hero'
import McpSection from '../components/home/McpTranscript'
import MoneyFlow from '../components/home/MoneyFlow'
import PayPalProof from '../components/home/PayPalProof'
import ProofStats from '../components/home/ProofStats'
import SeeItWork from '../components/home/SeeItWork'
import { useDemoInfo } from '../lib/demoInfo'

export default function Home() {
  const info = useDemoInfo()
  const demo = info === undefined ? undefined : info?.workshops === true
  const origin = typeof window === 'undefined' ? 'https://your-commitpool' : window.location.origin
  return (
    <>
      <title>CommitPool – find the date that fills the class</title>
      <Hero demo={demo} />
      <ProofStats />
      <SeeItWork />
      <AgentFeed />
      <MoneyFlow />
      <PayPalProof />
      <McpSection origin={origin} />
      <ForJudges info={info} />
      <Faq />
      <Closing demo={demo} />
    </>
  )
}
