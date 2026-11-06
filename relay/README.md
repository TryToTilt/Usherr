# Usherr Link Relay on Cloudflare

This folder is the relay linked Usherrs talk through, as a Cloudflare Worker
with Durable Objects. It only passes on sealed envelopes; it can't read them.
How it works: [docs/home/link-protocol.md](../docs/home/link-protocol.md).

The Usherr project runs it for everyone at `wss://relay.usherr.app`. These
steps are for whoever looks after that, on the **project's own Cloudflare
account** (not a personal one).

## Putting it online (first time)

You need: a computer with [Node.js](https://nodejs.org) 22 or newer, this
repository, and the login of the Usherr project's Cloudflare account (with
two-step sign-in on).

1. Open a terminal in this `relay` folder.
2. Install the tools: `npm install`
3. Sign in to Cloudflare: `npx wrangler login`. A browser page opens; sign in
   with the **project** account and press Allow. (Check with
   `npx wrangler whoami` that it shows the project account.)
4. Put the relay online: `npx wrangler deploy`. It prints an address like
   `https://usherr-relay.<something>.workers.dev`. Open it in a browser: it
   should say "Usherr Link Relay".
5. Make sure `usherr.app` is a website (zone) in the same Cloudflare account:
   Cloudflare dashboard → Add a domain → `usherr.app`, then at Porkbun change
   the domain's nameservers to the two Cloudflare shows. Wait until Cloudflare
   says the domain is Active (minutes to a few hours).
6. Give the relay its address: in `wrangler.toml`, remove the `#` in front of
   the three `routes` lines, save, and run `npx wrangler deploy` again.
   Cloudflare makes `relay.usherr.app` and its certificate by itself.
   (Or in the dashboard: Workers & Pages → usherr-relay → Settings → Domains &
   Routes → Add → Custom domain → `relay.usherr.app`.)
7. Check it: open `https://relay.usherr.app` in a browser; it says "Usherr
   Link Relay". Then in an Usherr: Settings → Movie night → Linked Usherrs →
   switch on Link with other Usherrs. It should say "Relay: Connected".

## Updating it

1. Get the new code (pull the repository).
2. In this folder: `npm install`, then `npx wrangler deploy`.

Connected Usherrs drop for a moment and reconnect by themselves. Messages
waiting for an offline Usherr are kept (they're in Durable Object storage).

## Trying it on your own computer

`npx wrangler dev` runs it at `ws://127.0.0.1:8787` (nothing goes online).
To check it follows the protocol, from the repository root:

```sh
ULR_TEST_URL=ws://127.0.0.1:8787 go test ./internal/link/ -run 'Conformance|TwoUsherrs'
```

## Good to know

- Cost: the free plan covers a lot of Usherrs. Idle connections cost nothing
  (the Durable Objects sleep). If it ever grows, the Workers Paid plan is
  $5 a month.
- No account id is in `wrangler.toml`: `wrangler login` decides the account,
  so the code can be public.
- Logs are off (`observability.enabled = false`): the relay keeps nothing
  about who talks to whom.
- To see usage: Cloudflare dashboard →
  Workers & Pages → usherr-relay.
- Type check after changing the code: `npx tsc --noEmit`.
