// Home fork: 👍 / 👎 for a title page, and the same two as a poster card's
// actions. Tapping the lit thumb again takes the vote back.
import type { CardAction } from '../components/PosterCard'
import Icon from '../components/Icon'
import { t } from '../i18n'
import { useTitleMatch, whyText } from './useTaste'
import { useVotes, type VoteKind, type VoteMeta, type Votes } from './useVotes'

export function voteActions(votes: Votes, kind: VoteKind, ref: string | number, meta: VoteMeta): CardAction[] {
  const v = votes.get(kind, ref)
  return [
    {
      icon: 'thumb-up',
      label: v === 1 ? t('You like this (tap to undo)') : t('I like this'),
      active: v === 1,
      onClick: () => votes.toggle(kind, ref, 1, meta),
    },
    {
      icon: 'thumb-down',
      label: v === -1 ? t('Not for you (tap to undo)') : t('Not for me'),
      active: v === -1,
      onClick: () => votes.toggle(kind, ref, -1, meta),
    },
  ]
}

export default function VoteButtons({ kind, refId, meta }: { kind: VoteKind; refId: string | number; meta: VoteMeta }) {
  const votes = useVotes()
  const v = votes.get(kind, refId)
  return (
    <span className="vote-buttons" role="group" aria-label={t('Your vote')}>
      <button type="button" className={`btn-sm btn-with-icon vote-up${v === 1 ? ' on' : ''}`} aria-pressed={v === 1} onClick={() => votes.toggle(kind, refId, 1, meta)} title={t('I like this')}>
        <Icon name="thumb-up" size={15} /> {v === 1 ? t('Liked') : t('Like')}
      </button>
      <button
        type="button"
        className={`btn-sm icon-btn vote-down${v === -1 ? ' on' : ''}`}
        aria-pressed={v === -1}
        onClick={() => votes.toggle(kind, refId, -1, meta)}
        title={v === -1 ? t('Not for you (tap to undo)') : t('Not for me')}
        aria-label={t('Not for me')}
      >
        <Icon name="thumb-down" size={15} />
      </button>
      {(kind === 'movie' || kind === 'tv') && <TasteMatch kind={kind} tmdbId={Number(refId)} vote={v} />}
    </span>
  )
}

// TasteMatch: how well a film or show fits your taste, and why (in its
// tooltip). Not shown once you've voted on it: your vote says more.
function TasteMatch({ kind, tmdbId, vote }: { kind: 'movie' | 'tv'; tmdbId: number; vote: number }) {
  const why = useTitleMatch(kind, tmdbId, vote)
  if (!why) return null
  const cls = why.match >= 70 ? ' good' : why.match < 40 ? ' poor' : ''
  return (
    <span className={`taste-match${cls}`} title={whyText(why)}>
      <Icon name="sparkles" size={13} /> {t('{n}% match', { n: why.match })}
    </span>
  )
}

// CardVotes: small thumbs along the bottom of a library card, visible without
// hovering (library cards already carry several actions on the picture).
export function CardVotes({ kind, tmdbId, title, year, image, match }: { kind: 'movie' | 'tv'; tmdbId: number; title: string; year?: number; image?: string; match?: number }) {
  const votes = useVotes()
  if (!tmdbId) return null
  const v = votes.get(kind, tmdbId)
  const meta = { title, year, image }
  return (
    <span className="card-votes" role="group" aria-label={t('Your vote')}>
      {match && !v ? (
        <span className="card-match" title={t('How well it fits your taste')}>
          {t('{n}% match', { n: match })}
        </span>
      ) : null}
      <button
        type="button"
        className={v === 1 ? 'on up' : ''}
        aria-pressed={v === 1}
        onClick={() => votes.toggle(kind, tmdbId, 1, meta)}
        title={v === 1 ? t('You like this (tap to undo)') : t('I like this')}
        aria-label={t('I like this')}
      >
        <Icon name="thumb-up" size={13} />
      </button>
      <button
        type="button"
        className={v === -1 ? 'on down' : ''}
        aria-pressed={v === -1}
        onClick={() => votes.toggle(kind, tmdbId, -1, meta)}
        title={v === -1 ? t('Not for you (tap to undo)') : t('Not for me')}
        aria-label={t('Not for me')}
      >
        <Icon name="thumb-down" size={13} />
      </button>
    </span>
  )
}
