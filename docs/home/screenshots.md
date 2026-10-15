# The pictures in this guide

*For whoever maintains the fork's documentation.*

The pictures in `docs/home/images` are taken by a script from a **demo
instance** of Mediarium: a separate container with its own fresh settings and
a demo library, never a real household's. Re-run it after changing a page, and
every picture is current again.

## The demo instance

A Mediarium container with its own config folder, for example:

```
docker run -d --name mediarium-docs -p 127.0.0.1:8267:8264 \
  -v /path/docs-demo/config:/config -v /path/docs-demo/data:/data \
  -v /path/docs-demo/media/movies:/movies -v /path/docs-demo/media/tv:/tv \
  -v /path/docs-demo/media/downloads:/downloads mediarium-home:<tag>
```

Then, once:

1. Make the administrator through the first-run page (a made-up name such as
   "Alex Demo"), and save the username and password in a login file outside
   the repository:

   ```
   username: alex
   password: ...
   ```

2. Add a TMDB key (and, for the AI and score pictures, an AI and an MDBList key).
3. Add a demo library: well-known films and shows from Discover. Pictures of
   downloaded titles need them marked downloaded; on a demo that has no files,
   that is done in its database with the container stopped.
4. Turn on every media type, and answer the usage-numbers question with *No*.

## Taking the pictures

```
tools/home/screenshots/run.sh mediarium-docs /path/docs-demo/login.txt
```

or only some of them:

```
tools/home/screenshots/run.sh mediarium-docs /path/docs-demo/login.txt dashboard extras-ai
```

The list is `tools/home/screenshots/shots.json`. Each entry has:

| Field | |
|---|---|
| `name` | The file name in `docs/home/images` (without `.png`) |
| `path` | The page to open |
| `viewport` | `desktop` (1440×900) or `phone` (390×844); desktop when left out |
| `waitFor` | Text to wait for before the picture |
| `card` | Picture only the card (or section) under this heading |
| `fullPage` | The whole page, however long |
| `callouts` | Texts to point out with numbered boxes |
| `before` | Steps first: `{ "click": "text" }`, `{ "press": "css selector" }` or `{ "scroll": "text" }` |
| `element`, `below` | Picture only this element (CSS selector), with `below` pixels more under it for an open menu |
| `as` | `viewer` to take it as a household member (`login-<name>.txt` next to the login file); the administrator otherwise |

The setup wizard's Extras step (`wizard-extras`) isn't in the list, since the
demo has finished its setup. To take it again: stop the demo, delete the
`onboarding.done` row from its `settings` table, start it, and run a one-off
list whose `before` presses `.wizard-nav button.primary` seven times (each
followed by an optional click on **Continue anyway**); then save
`{"onboardingDone": true}` to `/api/settings` again. Pictures of the tour
(`tour-…`) open the page with `?docs-tour`; on every other picture the tour is
kept away.

The browser runs in the official Playwright image (`mcr.microsoft.com/playwright`),
sharing the demo container's network. A picture that fails leaves
`_failed-<name>.png` with what the browser saw; those files are not committed.
