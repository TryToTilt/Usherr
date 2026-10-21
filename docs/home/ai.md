# The AI

The AI helps find something to watch, explains things, and builds Live TV
channels. It only suggests: every title it names is checked on TMDB, and
nothing is added, downloaded or changed without you. Without an AI, every
part of Mediarium still works; the AI buttons simply don't show.

## Setting it up

**Settings › Extras › AI**, then **Which AI**:

| AI | What it's like | Cost |
|---|---|---|
| **Ollama** (a model on your own computer) | Free and private; needs a graphics card to be quick. Install Ollama, then `ollama pull gemma3:4b`. The address is usually `http://host.docker.internal:11434`. The first answer after a while can take a minute while the model loads. | Free |
| **Claude** (Anthropic) | Knows films and shows best, answers in seconds. Needs an API key from console.anthropic.com. | Paid by use |
| **An OpenAI-compatible server** (LM Studio and others) | Any server that speaks the OpenAI API, at home or elsewhere. | Depends on the server |

- **Model**: left empty, the default is used (`gemma3:4b` for Ollama, Claude
  Sonnet for Claude). For Claude, `claude-haiku-5-5` is about twenty times
  cheaper than Sonnet and is plenty for questions about films and shows; use
  Sonnet when you want the best answers (such as building channels).
- **Answers a day for each person** keeps use in check (0 is no limit;
  administrators aren't limited).
- **Test it** asks a small question and shows the answer.

What it costs with Claude, roughly: a question in the Ask bar is a fraction
of a cent with Haiku and around a cent with Sonnet. Real TV's questions carry
your library's list; Mediarium keeps that part in Claude's cache, which is
billed at a tenth of the price when asked again soon.

![The AI card: which AI, its address and model, the daily allowance, Save and Test it](images/extras-ai.png)

## Asking

The bar at the top of the Dashboard, Discover, the Library and each media
page has a switch on its left: **Search** (the usual live search) or **Ask**.
Ask in your own words; the AI works out what you want and opens it right
there:

- **Find a title** from a description (*the one where the guy relives the
  same day*), or from a photo of a poster or a scene.
- **Tonight**: *What should I watch tonight?*, from what the house has.
- **Pick for the Room**: who's watching, a film or a show, a mood; three
  titles from the library that nobody in the room has seen, with a line on
  why.
- **Catch me up**: a spoiler-free recap of a show up to where you are (the
  last episode you finished in Jellyfin, or one you choose).
- **Skip guide**: the episodes of a show that matter, and the ones you can
  skip.
- **What's it like?**: tone, pace, intensity and who it's for (*is it too
  scary for a nine-year-old?*).
- Books: **books from a description**, **read-alikes**, and a recap of a
  book series; comics: **reading orders** and where to start, and a recap.
- Music: a **playlist** from your own music for a mood or an occasion, and
  **artists like** one you have.
- **Why a download failed** (also **Explain** on a failed download in
  Activity): what went wrong and what to try, in plain words. It only
  explains; it never acts.
- Anything else gets a short answer.

The same features also have their own buttons: on a title's page (*What's
it like?*, *Skip guide*, *Catch me up*), on the Dashboard (*What should I
watch tonight?*), on a book's or artist's page, and on the Comics page.

## Elsewhere

- **The calendar's Ask**: *anime this weekend*, *superhero films next month*;
  the matching entries are listed by date above the calendar.
- **Entertainment news**: each story summarised in a few lines (Settings ›
  Extras › Entertainment news; *Stories summarised a day* limits AI use).
- **The family filter's estimates** of ages, when Common Sense Media has
  none.
- **Live TV**: **Build with AI** makes a channel from a description, and
  **Real TV** asks the AI for stand-ins for what can't be replayed (the local
  news, a live game). Real TV remembers its choices and asks only about new
  slots, so after the first build it costs little or nothing. See
  [Live TV](live-tv.md).

## When something is off

See *The AI is slow, or says it can't be reached* in
[When something goes wrong](troubleshooting.md).
