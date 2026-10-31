// Usherr SMH: turn an ebook into an audiobook with Read-to-Me.
import { t } from '../../i18n'
import type { SmhStep, Walkthrough } from '../types'

const start: SmhStep[] = [
  {
    kind: 'go',
    to: '/read-to-me',
    title: t('Read-to-Me'),
    text: t('Turns an ebook into an audiobook with a computer voice, made here on your own computer. The audiobook goes in your Audiobooks library, with chapters and the cover, and plays in Usherr, Audiobookshelf and phone apps.'),
  },
  {
    kind: 'point',
    target: 'rtm-pick',
    until: { path: '/read-to-me/*' },
    title: t('Pick the book'),
    text: t('Only books that are downloaded as EPUB can be read aloud. Tap the one you want.'),
  },
]

const voice: SmhStep = {
  kind: 'point',
  target: 'rtm-voice',
  until: 'next',
  title: t('Pick a voice'),
  text: t('▶ on a voice plays a short sample. Everyday voices are quick to make; expressive ones sound more human but take a lot longer.'),
}

const hear: SmhStep = {
  kind: 'point',
  target: 'rtm-hear',
  until: 'next',
  title: t('Hear a sample'),
  text: t('Plays about 200 words of the book in that voice, before you commit. It takes a few seconds to make.'),
}

const make: SmhStep = {
  kind: 'point',
  target: 'rtm-make',
  act: 'click',
  until: 'next',
  title: t('Make the audiobook'),
  text: (c) => (c.watch ? t('Starting it for you.') : t('Press Make the audiobook.')),
}

const done: SmhStep = {
  kind: 'done',
  title: t('Your audiobook is being made'),
  text: t('It takes a while: the page says how long. You can leave, it carries on. Listen while it is being made plays the chapters already done, and the finished book appears in Audiobooks.'),
  shortcut: t('To get a notice when it’s done: Settings, Notifications, tick Audiobooks made.'),
}

export const ebookToAudiobook: Walkthrough = {
  id: 'ebook-audiobook',
  group: 'listen',
  needs: 'ebooks',
  admin: true,
  title: t('Turn an ebook into an audiobook'),
  blurb: t('Read-to-Me reads a book aloud with a computer voice and saves it as an audiobook.'),
  icon: 'headphones',
  words: 'read to me readtome audiobook ebook voice narrator tts listen book aloud',
  simple: [...start, voice, hear, make, done],
  advanced: [
    ...start,
    {
      kind: 'point',
      target: 'rtm-mode',
      until: 'next',
      title: t('Simple or Full'),
      text: t('Simple shows the three steps that matter. Press Full to see all six: you’ll walk through each.'),
    },
    {
      kind: 'point',
      target: 'rtm-guide',
      until: 'next',
      title: t('Guide me'),
      text: t('Lights one step at a time and dims the rest. Leave it on if you like it; it works alongside this walkthrough.'),
    },
    {
      kind: 'point',
      target: 'rtm-book',
      until: 'next',
      title: t('Book: what’s read'),
      text: t('Which chapters, whether the title and chapter titles are read, and the pauses after paragraphs, scene breaks and chapters. The contents and copyright pages are left out by themselves.'),
    },
    {
      kind: 'point',
      target: 'rtm-voice',
      until: 'next',
      title: t('Narrator'),
      text: t('The voice, or a preset (a saved way of reading, like Warm narrator). With an everyday voice, Mix in a second voice blends two.'),
    },
    {
      kind: 'point',
      target: 'rtm-cast',
      until: 'next',
      when: () => !!document.querySelector('[data-walk="rtm-cast"]'),
      title: t('Cast: voices for the dialogue'),
      text: t('One voice for everything, a second voice for what’s in quotation marks, or a voice for each character. Usherr finds the characters and who says each line.'),
    },
    {
      kind: 'point',
      target: 'rtm-sound',
      until: 'next',
      when: () => !!document.querySelector('[data-walk="rtm-sound"]'),
      title: t('Sound'),
      text: t('The speed (0.8× to 1.3×) and how the reading sounds. Change one thing at a time and press Hear a sample.'),
    },
    {
      kind: 'point',
      target: 'rtm-words',
      until: 'next',
      when: () => !!document.querySelector('[data-walk="rtm-words"]'),
      title: t('Words: say it right'),
      text: t('Names a voice gets wrong can be written how they sound: Hermione → her-MY-oh-nee. For this book, or for every book.'),
    },
    hear,
    {
      kind: 'point',
      target: 'rtm-make',
      until: 'next',
      title: t('Make it, and presets'),
      text: t('The Make it step shows how long the book is and how long making it should take. Save these settings as a preset to use them again.'),
    },
    make,
    done,
  ],
}
