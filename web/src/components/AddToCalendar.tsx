import { buttonClass, Icon } from './ui'

export default function AddToCalendar({ path }: { path?: string }) {
  if (!path) return null
  return (
    <a href={path} download className={`${buttonClass('secondary', 'sm')} self-start no-underline`}>
      <Icon name="calendar" size={16} />
      Add to calendar
    </a>
  )
}
