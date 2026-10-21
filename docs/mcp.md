# Ask your library questions from a chat or a voice assistant (MCP)

Mediarium can talk to chat programs and voice assistants that support MCP, the Model Context Protocol. Once one is connected you can ask things like:

- "Which of my movies are not FuN releases yet?"
- "How many movies are over 15 GB?"
- "Which downloads failed this week? Try another release for the first one."
- "Show the latest requests from Sam."
- "Move Tide Pool to the 4K profile."
- "Add the show with TVDB id 123."

Questions are answered straight away. Anything that changes something is shown first and only happens after you say yes.

It is off until you switch it on, and it adds nothing to install: it is part of Mediarium.

## Switch it on

1. Open **Settings > Connections > Other apps**.
2. Under **Let AI assistants connect (MCP)** switch it on. The address appears with a copy button.
3. Make an API key for the assistant: press **Make a key for other apps** on the same page, or use **Accounts > API keys**. For a key that may only ask questions, tick **Read-only** when you make it.

Only an administrator's key works. A key of a basic account is refused. Switching MCP **on** can only be done from the web page while you are signed in, never with an API key, so a leaked key can't open the door by itself.

## Connect your assistant

The address is `http://<mediarium>:8264/mcp` (use your own host and port; behind a reverse proxy with `URL_BASE`, put that path in front: `https://home.example.com/mediarium/mcp`). Mediarium also answers at `/api/mcp`. The key goes in the `Authorization` header as `Bearer <your key>`. The `X-Api-Key` header works too.

Most programs that support MCP over the web take a block like this in their settings file:

```json
{
  "mcpServers": {
    "mediarium": {
      "url": "http://192.168.1.20:8264/mcp",
      "headers": { "Authorization": "Bearer YOUR_API_KEY" }
    }
  }
}
```

Some programs use a `servers` block and want the kind spelled out:

```json
{
  "servers": {
    "mediarium": {
      "type": "http",
      "url": "http://192.168.1.20:8264/mcp",
      "headers": { "Authorization": "Bearer YOUR_API_KEY" }
    }
  }
}
```

A program that can only start a local command, not call an address, can go through the small `mcp-remote` bridge:

```json
{
  "mcpServers": {
    "mediarium": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "http://192.168.1.20:8264/mcp", "--header", "Authorization: Bearer YOUR_API_KEY", "--allow-http"]
    }
  }
}
```

**Home Assistant** (voice): add the *Model Context Protocol* integration and give it the address. It has to send the key as an `Authorization: Bearer` header; look in your version's integration page for where that goes. Then pick the Mediarium tools for your voice assistant.

To try it without any assistant, ask Mediarium directly:

```
curl -s http://192.168.1.20:8264/mcp \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"library_stats","arguments":{}}}'
```

## What it can do

**Questions** (any administrator key, read-only ones too):

| Tool | What it answers |
|---|---|
| `library_search` | Find titles. Filters: title, kind (movies, tv, music, books), quality, smallest and largest size in GB, release group, audio language and video codec (from the tracks inside the files), has a file, monitored. Gives the total and a page of results (up to 100), so it also counts: "how many movies over 15 GB" is one call. |
| `library_stats` | Totals, space used, split by quality, movies by file size and the most common release groups. |
| `lookup_title` | Looks a movie or show up by name to get its TMDB id. |
| `movie_details`, `series_details` | One title: file, quality, size, release group and release name; for shows the seasons and the aired episodes that are missing. |
| `wanted_list` | What is still being looked for, or still below the quality the profile wants. |
| `queue_status` | The download queue with progress. |
| `recent_history` | The newest lines of Activity, optionally only the problems. |
| `downloads_failed` | Failed downloads with the reason. |
| `health_report` | The same health items as the dashboard. |
| `requests_list` | Requests from the request portal, by person, status or kind. |
| `profiles_list` | Quality profiles with their qualities (and their own order and groups, when they have one), size limit, preferred terms and custom formats. |
| `release_group_report` | The titles that are **not** from a given release group, for movies or TV. |

Sizes are in GB of 1024 MB, the way file managers show them. The release group comes from the release name; for titles that were imported from an existing library it is read from the file name, and titles where it can't be told are counted as unknown.

**Changes** (an administrator's key that is not read-only):

| Tool | What it does |
|---|---|
| `add_movie` | Adds a movie by TMDB id, on a profile you name, and searches for it. |
| `add_series` | Adds a show by TMDB or TVDB id, with the monitoring you choose. |
| `search_now` | Searches right now for up to 20 movies, shows or episodes. |
| `set_movie_profile`, `set_series_profile` | Moves a title to another quality profile ("upgrade it to 4K"). If its file is below the new profile, a search for a better one starts. |
| `retry_failed_download` | Blocks the release of a failed download and searches for another. |
| `add_preferred_term` | Adds a preferred term with a score to a profile, or changes the score of one that is there. |

Each of these needs `confirm: true`. Called without it, the tool changes nothing and answers with what it would do, and tells the assistant to ask you. Only when you agree does the assistant call it again with `confirm` set. The changes run through the same code as the web pages, so the same checks apply (a title that is already in the library is refused, a profile that doesn't exist is refused, and so on).

Besides the tools, Mediarium offers two resources (a library summary and the health items) and three ready-made prompts (a health check, a look at failed downloads, and a release group check). Programs that show prompts list them.

## Good to know

- **The yes is the assistant's to give.** Mediarium can't see whether you really said yes in the chat; it only sees `confirm: true` in the call. An assistant that is told to, or tricked into it by text it reads, could send it without asking. If you don't fully trust a program, give it a **read-only key**: it then only sees the question tools and can't change anything.
- **Make a key just for it**, named after the program. Revoke it under **Accounts > API keys** and it stops working at once. Switch MCP off on the Other apps page and the address stops answering (404) for every key.
- **Your library leaves the house.** What a tool answers (titles, file names, sizes, request names) goes to the assistant, and from there to whoever runs it. A voice assistant or chat program that runs fully on your own machine keeps it at home; a hosted one does not.
- **Limits.** A request can be up to 256 KB; one key can send about 20 requests a second (with a burst of 100) before it is asked to wait; a long answer is cut and says so. Nothing is kept between requests (no sessions).
- **The log** has one line per tool call with the tool's name, how it went and how big the answer was. It never has the arguments or the key.
- **Not here yet:** music and books can be searched by name, but they have no change tools, and there is no way to delete anything.
