/**
 * Falconia's mark: a falcon's head in profile — hooked beak, the dark "tear"
 * stripe under the eye — in the gold of the city's banners. Plain paths, so it
 * holds from a favicon to a hero.
 */
export function LogoMark({ size = 32 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" aria-hidden="true">
      <rect width="64" height="64" rx="14" fill="#11141b" />
      <path d="M15 54 C13 41 16 28 25 19.5 C31 14 40 12 47.5 15 C52.5 17 55.5 21 56 26 C56.3 28.6 55 30.4 52.8 30.7 C53.3 28.7 52.4 27.1 50.2 26.9 L46.6 27.7 C47.5 34 45 41 39 46 C33.5 50.5 25 53.5 15 54 Z" fill="#d4a93a" />
      <path d="M40.5 19.5 C37.5 22 36 26 36.5 31 C36.8 34.5 35 37.5 32 39.5 C36 39.2 39.5 36.5 40.5 32.5 C41.3 29 41.3 24.5 40.5 19.5 Z" fill="#11141b" />
      <circle cx="44.2" cy="21" r="2.5" fill="#11141b" />
      {size >= 40 && <circle cx="44.9" cy="20.3" r="0.75" fill="#f5e6b8" />}
    </svg>
  )
}

export function Logo({ size = 28 }: { size?: number }) {
  return (
    <span className="inline-flex items-center gap-2">
      <LogoMark size={size} />
      <span className="font-semibold tracking-tight text-white">Falconia</span>
    </span>
  )
}
