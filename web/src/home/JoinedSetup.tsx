// Usherr: Community. A friend's watch party, joined with its showtime code,
// that isn't linked to its thread yet: what it takes for its chat to show
// here and the host's stamp to reach your stub. Your Usherr needs its own
// bot; the host adds that bot to their Discord server with its invite link;
// and you link your Discord account here. Usherr then finds the party by
// itself, by its code (home_community_joined.go).
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { copyText } from '../components/CopyBox'
import { t } from '../i18n'

export interface JoinedParty {
  waiting?: boolean
  bot?: 'on' | 'off'
  invite?: string
  needLink?: boolean
}

export function JoinedSetup({ party, title, admin }: { party: JoinedParty; title: string; admin: boolean }) {
  const [copied, setCopied] = useState<boolean | null>(null)
  if (!party.waiting && !party.needLink) return null
  return (
    <section className="card joined-setup">
      <h2>{t('Your friend’s watch party: {title}', { title })}</h2>
      {party.waiting ? (
        <>
          <p>{t('To see its chat here, talk in it, and have your ticket stub stamped when the host’s bot stamps it:')}</p>
          <ol>
            <li>
              {party.bot === 'on' ? (
                t('Your Usherr’s own Discord bot is connected. ✓')
              ) : admin ? (
                <>
                  {t('Give your Usherr its own Discord bot: ')}
                  <Link to="/settings/community">{t('Set up the bot')}</Link>
                  {t(' (Show me how there walks you through it).')}
                </>
              ) : (
                t('Your Usherr needs its own Discord bot. Ask whoever runs it to set one up (Settings, Movie night, Community).')
              )}
            </li>
            <li>
              {t('Send your bot’s invite link to the friend hosting the party. They open it, choose their server and press Authorize: that adds your bot there.')}
              {party.invite && (
                <span className="joined-copy">
                  <button
                    type="button"
                    className="btn-sm primary"
                    onClick={() => {
                      const link = party.invite ?? ''
                      void copyText(link).then(setCopied)
                    }}
                  >
                    {t('Copy the invite link')}
                  </button>
                  {copied === true && <span className="hint">{t('Copied. Paste it to them in Discord.')}</span>}
                  {copied === false && <input readOnly value={party.invite} aria-label={t('Your bot’s invite link')} onFocus={(e) => e.currentTarget.select()} />}
                </span>
              )}
            </li>
            {party.needLink && <LinkStep />}
          </ol>
          <p className="hint">{t('Once your bot is in their server, Usherr finds the party by its code within a couple of minutes. Nothing else to do.')}</p>
        </>
      ) : (
        <ol>
          <LinkStep />
        </ol>
      )}
    </section>
  )
}

function LinkStep() {
  return (
    <li>
      {t('Link your Discord account, so what you type carries your name and the host’s stamp reaches your stub: ')}
      <Link to="/settings/services#discord">{t('Linked services')}</Link>
      {t('. In their server, use the /link of your own bot (Discord shows each bot’s name beside its commands).')}
    </li>
  )
}
