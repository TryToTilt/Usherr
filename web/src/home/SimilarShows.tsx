import PosterCard from '../components/PosterCard'
import type { DiscoverMovie } from '../api'
import { t } from '../i18n'

// Usherr: More like this, on a show's page (this one and the library's).
export default function SimilarShows({ shows }: { shows: DiscoverMovie[] }) {
  if (shows.length === 0) return null
  return (
    <section>
      <div className="rail-head">
        <h2>{t('More like this')}</h2>
      </div>
      <div className="poster-grid">
        {shows.map((m) => (
          <PosterCard key={m.tmdbId} to={`/show/${m.tmdbId}`} poster={m.posterUrl} title={m.title} meta={m.year || undefined} genres={m.genres} rating={m.rating} kind="tv" />
        ))}
      </div>
    </section>
  )
}
