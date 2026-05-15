import { type ReactNode, type SVGProps } from 'react'

interface IconProps extends Omit<SVGProps<SVGSVGElement>, 'children' | 'd' | 'stroke'> {
  size?: number
  stroke?: number
}

interface SvgProps extends IconProps {
  d: ReactNode
}

function Svg({ d, size = 16, stroke = 1.6, style, ...rest }: SvgProps) {
  return (
    <svg
      fill="none"
      height={size}
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth={stroke}
      style={{ display: 'block', flex: '0 0 auto', ...style }}
      viewBox="0 0 24 24"
      width={size}
      {...rest}
    >
      {d}
    </svg>
  )
}

export function IconPower(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <path d="M12 4v8" />
          <path d="M7.5 7.5a6.5 6.5 0 1 0 9 0" />
        </>
      }
    />
  )
}

export function IconCog(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <circle cx="12" cy="12" r="3" />
          <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3h0a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5h0a1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8v0a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
        </>
      }
    />
  )
}

export function IconChevron(p: IconProps) {
  return <Svg {...p} d={<path d="M9 6l6 6-6 6" />} />
}

export function IconChevronD(p: IconProps) {
  return <Svg {...p} d={<path d="M6 9l6 6 6-6" />} />
}

export function IconPlus(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <path d="M12 5v14" />
          <path d="M5 12h14" />
        </>
      }
    />
  )
}

export function IconMore(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <circle cx="5" cy="12" r="1" />
          <circle cx="12" cy="12" r="1" />
          <circle cx="19" cy="12" r="1" />
        </>
      }
    />
  )
}

export function IconAlert(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <path d="M12 9v4" />
          <path d="M12 17h.01" />
          <path d="M10.3 3.86 1.82 18a2 2 0 0 0 1.7 3h16.94a2 2 0 0 0 1.7-3L13.7 3.86a2 2 0 0 0-3.4 0z" />
        </>
      }
    />
  )
}

export function IconRefresh(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <path d="M3 12a9 9 0 0 1 15.3-6.4L21 8" />
          <path d="M21 3v5h-5" />
          <path d="M21 12a9 9 0 0 1-15.3 6.4L3 16" />
          <path d="M3 21v-5h5" />
        </>
      }
    />
  )
}

export function IconCheck(p: IconProps) {
  return <Svg {...p} d={<path d="M20 6 9 17l-5-5" />} />
}

export function IconX(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <path d="M18 6 6 18" />
          <path d="m6 6 12 12" />
        </>
      }
    />
  )
}

export function IconRoute(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <circle cx="6" cy="19" r="3" />
          <circle cx="18" cy="5" r="3" />
          <path d="M6 16V8a4 4 0 0 1 4-4h4" />
          <path d="M18 8v8a4 4 0 0 1-4 4h-4" />
        </>
      }
    />
  )
}

export function IconEdit(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <path d="M12 20h9" />
          <path d="M16.5 3.5a2.1 2.1 0 1 1 3 3L7 19l-4 1 1-4Z" />
        </>
      }
    />
  )
}

export function IconCopy(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <rect height="13" rx="2" width="13" x="9" y="9" />
          <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
        </>
      }
    />
  )
}

export function IconTrash(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <path d="M3 6h18" />
          <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
          <path d="M19 6 18 20a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
        </>
      }
    />
  )
}

export function IconLink(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <path d="M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07l-1 1" />
          <path d="M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07l1-1" />
        </>
      }
    />
  )
}

export function IconSearch(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <circle cx="11" cy="11" r="7" />
          <path d="m21 21-4.3-4.3" />
        </>
      }
    />
  )
}

export function IconSun(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <circle cx="12" cy="12" r="4" />
          <path d="M12 2v2" />
          <path d="M12 20v2" />
          <path d="m4.93 4.93 1.41 1.41" />
          <path d="m17.66 17.66 1.41 1.41" />
          <path d="M2 12h2" />
          <path d="M20 12h2" />
          <path d="m4.93 19.07 1.41-1.41" />
          <path d="m17.66 6.34 1.41-1.41" />
        </>
      }
    />
  )
}

export function IconMoon(p: IconProps) {
  return <Svg {...p} d={<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79Z" />} />
}

export function IconDisplay(p: IconProps) {
  return (
    <Svg
      {...p}
      d={
        <>
          <rect height="14" rx="2" width="20" x="2" y="3" />
          <path d="M8 21h8" />
          <path d="M12 17v4" />
        </>
      }
    />
  )
}

export function Flag({ code }: { code: string }) {
  return <span className="flag">{code}</span>
}
