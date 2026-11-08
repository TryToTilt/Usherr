# Usherr changelog

Usherr's own releases. Each one is built on a Mediarium release by rdborg,
whose changes are in [CHANGELOG.md](CHANGELOG.md).

## [Unreleased]

## [2.63.0] - 2026-11-08

### Changed

- **A manga pack downloads only the volumes you're missing.** When a torrent pack arrives, its files for volumes you already have, or unticked, are left out, as a TV pack leaves out the other seasons; a pack taken to swap one volume fetches only that one. Files whose names don't say which volume they are still come down. The queue now shows a pack as "Volumes 1–41" instead of the one volume it was found for.

## [2.62.1] - 2026-11-07

### Fixed

- **GitHub's checks pass again:** the MangaDex credit is written by the licence-list tool instead of by hand, and an unused lookup in the manga search is gone.

## [2.62.0] - 2026-11-07

### Show me how

- **Walkthroughs for everything new since 2.40:** link with a friend's Usherr, host a screening (lobby, intermission, verdict), use your phone at a screening, start a crew, pick the next film with your crew, see your badges and save a stub, get a manga series, set up manga, read manga, find manga and read it online, connect your MangaDex account, tidy up your music, play your music in a phone app, and get a league's games by themselves.
- **Walkthroughs brought up to date:** linked screenings in Book a showtime, Chat and Ticket stubs; the new readers for ebooks, audiobooks and comics; Simple quality profiles; sports downloads; Manga's own Discover.
- **The setup wizard explains the VPN step,** and its Extras step says where watching with friends and manga are set up. A new tip suggests a crew when you're invited to one or have linked Usherrs, and Settings > Extras > Sports has a Show me how button.

### Guide and website

- **The Show me how website covers everything up to 2.61,** with 14 new pages (linked Usherrs, hosting a screening, your phone at a screening, crews, the next-film vote and film clubs, badges, sports downloads, phone apps for music, music tidy-up, and setting up, getting, discovering and reading manga, plus MangaDex). Joining a watch party, party chat, ticket stubs and older pages (media types, sports, page layouts, seasons, Leaving soon, quality profiles, the readers, notices) are brought up to date.
- **The Start page's setup file can have a Manga folder,** the NVIDIA option also lets Live TV use the graphics card, and the page lists what's in Usherr now.
- **The in-app guide and the docs cover everything up to 2.61.** New pages: **Watching together** (linked Usherrs, screenings in step, the party, crews, film clubs and Crew Wrapped) and **Running the server** (watch history, service checks, clean-up rule sets, Simple and Advanced profiles, tags, signing in with Jellyfin or Plex). Live TV's encoder and watermark, collections and poster badges, the new layout, Continue watching, the four new badges and installing with a graphics card are written up too, and the README lists what Usherr does today.
- The guide no longer says NSFW 18+ is for administrators only: accounts given its permission see it too, from its own page only.

## [2.61.2] - 2026-11-07

### Fixed

- **A manga pack you take stops single volumes being fetched beside it.** While a pack runs, every volume its name holds counts as on its way, so the regular search no longer downloads those volumes one by one as well.

## [2.61.1] - 2026-11-07

### Fixed

- **A new MangaDex account syncs.** With no reading statuses yet, MangaDex answers in a slightly different shape, and Usherr took it for MangaDex being away: syncing, browsing and chapter downloads all waited up to an hour. That answer is read now, and an answer Usherr can’t read is reported instead of pausing everything.

## [2.61.0] - 2026-11-07

### Added

- **Connect your own MangaDex account to Manga.** Optional, and each person connects their own at Settings > MangaDex account, with a personal API client from mangadex.org. The sign-in is saved encrypted and never shown again. Your follows, read chapters, reading status and lists then stay the same in Usherr and on MangaDex. Series you follow there come into Manga, and the Manga page shows new chapters of what you follow, what you read lately on MangaDex, and your lists. 18+ series stay out unless the NSFW section is on for you. Heavy syncing waits for the night, between 2 and 9. Disconnect deletes everything Usherr kept from it.
- **Manga > Discover is the one place to find manga.** It keeps AniList's shelves and adds a search at the top (AniList, and MangaDex when it's on, with a title both know shown once), and a **New official volumes** row from what your indexers brought in. Every tile has the same look, with **Add** and, with MangaDex on, **Read now**.
- **MangaDex in Discover**, when its source is on in Settings > Manga: popular new titles, the latest chapters with the group who translated each, the staff's seasonal, recommended and self-published lists, and recently added. **More filters** searches MangaDex by tags, status, readers, original language, year and sort, and **Random title** picks one. 18+ titles stay out unless the NSFW section is set up, your account may see it, and you tick the switch.
- **Read MangaDex chapters online** in Usherr's own manga reader, without downloading: curl, panels, direction and all. Usherr fetches the pages and passes them on, reporting each one to MangaDex's page servers as their rules ask, and keeps them half an hour at most. Your place is kept, a chapter picker sits in the bar, the next chapter is a press away, and **Download this chapter** hands it to the usual chapter download.
- **New reading options in both the Manga and Comics readers:** a **Wide strip** that runs across, a progress bar that can be normal (now with a tick a page), a thin line or hidden, at the bottom, left or right, a page number when it's hidden, hide the header (M brings it back), tap and pointer hints, greyscale, dim pages, a white, black or theme background, and a list of keyboard shortcuts. Kept per series. Nothing changes until you choose.

## [2.60.1] - 2026-11-07

### Changed

- **NSFW 18+ is only on its own page.** It’s no longer a choice in Discover’s or the Library’s media picker, even for administrators. The NSFW 18+ link in the menu still opens it.

## [2.60.0] - 2026-11-07

### Added

- **Ticket stubs over the link.** When a linked Usherr's screening starts, its host hands your Usherr a stub for each of your people who said I'm in, numbered by the host and signed with its key. A stamp you make at home goes back to the host, and the host confirms it, so both sides agree. Nobody outside the screening sees any of it.
- **Collectible stubs.** Each stub shows the film's poster (the library's own when the film is there), the date, time, row and seat, who hosted, and its number. Stamped, it gets a round **SEEN** stamp with your stars if you rated the film. **Save as picture** makes a PNG of the stub on your computer; Usherr never posts it anywhere.
- **Badge cabinet** under You, Ticket stubs: First party, Five nights, Regular, Season ticket, Perfect attendance (every party you were invited to in a month, three or more), Night owl, House lights and Double feature, each with how to earn it and when you did. Parties at linked Usherrs count too. Loudest scream, Class clown, Good taste and Generous are on the shelf as coming soon.
- **Chat with everyone at a linked screening, no Discord needed.** The party chat on the Showtime page and on your phone now reaches every Usherr at the screening. Mark a message as a spoiler and it stays blurred until someone taps it.
- **Reactions pinned to the moment.** Tap HA, EEK, WOW, NOPE or CLAP and it floats up over the picture in every house at once. Each one is kept at its second of the film, so anyone who watches that part later sees it float up too, and small bars under the picture show where the crew reacted most.
- **A phone page for the screening.** What's playing and where it's got to, the chat, the five reactions, and for the host a remote: pause or play for everyone and back 10 seconds.
- **Crews: your movie-night group between parties.** Make a crew with Usherrs you're linked with (Movie night > Crews). Each one gets an invite, and in each house only the people who join it themselves are in it.
  - **Chat** that keeps going between parties. Plain text, up to 500 characters.
  - **A shared watchlist.** Anyone in the crew can add a film. Each one shows who added it and whether each house has it: yes, getting it, or not yet. Nothing else about anyone's library is shared. **Add to my list** puts it on your own list, and **Send to** sends it to a friend: it shows in the chat as a card, and their Usherr lets them know.
  - **Vote on the next film.** Pick films from the shared watchlist and when it plays. A film not everyone can play is marked. One vote each. When voting closes, the winner is booked in Showtime for the crew, with the pre-show whoever started the vote picked.
  - **Film clubs.** A night, every week or every other week, and who picks: turns, or the crew's vote. Join or leave any time. Two days before each night, Usherr books the pick or opens the vote, and keeps count of the nights in a row.
  - **What the crew is watching**, for people who turn on **Share what I watch with my crews** themselves. It's off until you do, and nobody can turn it on for you. Nothing NSFW, ever.
- **Four more badges.** Loudest scream (the most EEKs of anyone at one party, three or more), Class clown (the most HAs of anyone in a month, ten or more), Good taste (a film you picked or voted for wins a crew vote three times) and Generous (five stars for ten films or shows) are now earned like the rest.
- **Crew Wrapped.** Each crew gets its year in review on the Crews page: the button shows from December 1 through January, and **Past Wrapped** opens earlier years. Five slides: how many parties and hours the crew had together and on which nights, the crew's favourite and most divisive film from the stars people shared, the year's most-reacted moment, whose picks won the votes, and next year. **Save as picture** makes a PNG of any slide on your computer; Usherr never posts it anywhere. A year without a party says so instead of showing zeros.
- **A lobby before the film.** The Showtime page counts down to the film, shows which part of the pre-show is playing in step, and has a seat map of who's coming. Press **Take my seat** when you're settled in; everyone else shows as on the way. Your seat is the one printed on your stub. Works for your own showtimes too.
- **Intermission.** The host of a linked screening can call an intermission from the player, the Showtime page or the phone page. The film pauses everywhere, everyone sees a 10-minute countdown and when it's back, and the host can add 5 minutes or resume for everyone. Guests can say what they're up to: getting snacks, refilling drinks, a quick break or back in their seat.
- **The verdict at the credits.** When the film ends (or the host presses **Roll credits**), everyone gives it stars, half stars too, and a line if they like. It goes to your own Diary, and to your review if you tick that. The crew's card shows the average and how many voted, with names only for people who choose to share, plus the most-reacted moment. Then **Collect your stub**, stamped with your stars.

### Fixed

- **Manga volumes packed as ZIPs of a RAR set are read.** Publishers’ releases often put each piece of a RAR set in its own ZIP. Usherr opened the ZIPs but not the set inside, so the volume failed with "holds only R00, R01, RAR, ZIP files". A **Retry** on such a download now finishes it without fetching it again.
- **Testing an indexer set to books only works.** Manga’s Nyaa searches only its English-translated literature, and **Test** asked it for films and TV, so it always said "returned no releases" though manga searches worked. Test now asks once more for anything at all.
- **A guest's stub gets stamped.** **Stamp it** now works for everyone at a party, host and guests, from the film's start until 30 minutes after it ends, instead of only in the half hour after the credits. Usherr's player stamps it at 90% of the film instead of 95%, and that now includes the stubs your Discord account keeps here.

## [2.59.1] - 2026-11-07

### Fixed

- **Manga downloads show the series and its cover in Downloads**, with the volume when it's one volume, instead of the release name and a stand-in picture.
- **One time left, not two.** A running download showed an estimate from its average speed beside one from its speed right now, and they could be half an hour apart. Only the one from the speed right now is shown.
- **Opening a download's details no longer pulls its cover down** to the middle of the card.

## [2.59.0] - 2026-11-07

### Added

- **Manga: chapters from MangaDex, if you choose them.** It's off until you switch it on in Settings > Manga, and official editions stay the default. A new **Chapters** tab on a series lets you follow new chapters or all of them. No account is needed.
- **Every chapter says what's true of it:** on disk, wanted, in a volume you have, on another site (those can't be downloaded, so they're skipped), or taken down.
- **"Why nothing yet?" now explains the empty ones.** When MangaDex lists a series but nothing can be downloaded, it says how many chapters were taken down and how many are links elsewhere, and that a publisher is the likely reason. A series with nothing is asked about less and less.
- **Chapters credit who made them.** Files go in `Series/Chapters/`, named with the group, with the group and MangaDex in their ComicInfo.xml. When several groups did a chapter, Usherr takes the one you chose for the series, or else the one that did the most.
- **Downloads are gentle.** One batch at a time and one line in Activity. A batch doesn't hold a place in the download line or use Usenet connections, and it carries on after a restart. Big catch-ups run between 2 and 9 in the morning. If MangaDex is away or asks Usherr to wait, it isn't asked until the time is up, and the page says so.
- **Reading runs across chapters and volumes.** The last page offers whichever comes next, and your place and Continue follow you.
- **When a volume arrives,** its chapters show "In Volume N" and your place carries over. **Tidy chapters** sets them aside in `.replaced` and never deletes them, and you can put them back.
- **Manga reads panel by panel, the same as Comics.** Press **Panels** in the reader and the page glides from panel to panel: right to left for manga, top to bottom for manhwa, remembered for each series. A picture holding two pages is read one page, then the other. A long manhwa strip is cut at its gaps, and shown a screen at a time where a stretch is too tall. A page with no panels to find is shown whole, and on an upright phone a wide one with none is shown a page at a time. Chapters read this way as well as volumes. PDFs aren't drawn on the server, so they read by the page.
- **More for reading manga**, all under the sliders button in the reader, and kept simple:
  - **Shift the pairs by one page**, for a series whose two-page pictures come out split across a turn. Kept for each series.
  - **Fit**: whole page, width, height or actual size. Too big for the screen, it scrolls.
  - **Trim white borders**, **Brightness** and **Dark pages**, which turns white paper black for reading in the dark.
  - **Hold a finger on a page** (or right-click) to save it. The screen stays on while you read, and goes to sleep after 15 minutes of no page turns. Tap zones are shown the first time, and the next pages load ahead of you. The page number now has a percentage beside it, and there's a full screen button.
- **Discover manga** (Manga > Discover): what's trending, popular or new on AniList, by genre if you like, with no key or account. **Add** on a tile is one press, and you choose what it gets: all volumes, new ones only or nothing yet. Type a user name to see what's on a public AniList list.
- **A series' page knows more**: its genres, score, **Related series** (sequels, spin-offs, side stories) and **More like this**, each with an Add button. This comes from AniList when you open the page and isn't stored, so without AniList the page simply doesn't have it.
- **New volumes of a series you follow** go on the calendar on the day they're noticed, and send a notification. If you've chosen notifications by hand, tick **New manga volumes** in Settings > Notifications. AniList often has no volume count until a series ends, so most new volumes are noticed from your indexers' releases.
- **Continue reading on Home**, for manga you're part way through, and how many volumes you've read on the Manga page.

### Changed

- A run like `One Piece (Chapters 1-721)` is still turned down for a series that follows volumes. For one that follows chapters, with fan scans from your indexers on, it now waits for you. Its chapters are filed if each is its own file. One big file can't be split, and Activity says so.
- **A manga series' details now refresh between 2 and 9 in the morning**, instead of at any hour. Running series are asked about daily, finished ones monthly.
- **Comics' panel view reads two-page pictures one page at a time too**, for pages it hasn't worked out yet, as it shares this code with Manga.

### Fixed

- **Manga volumes wrapped in a ZIP are read.** Published volumes often come as a RAR holding a ZIP, and Usherr said they held "only RAR, ZIP files". A ZIP of pages now counts as a CBZ, and a ZIP holding a PDF, EPUB or comic is opened.
- **More indexer sites work, including Nyaa.** Sites whose entries in the site list use the fancier kinds of text patterns now load and read properly, instead of being refused with an error or having some of their titles left untidied.
- **A manga series' dialogs open on the screen.** On a phone, **Choose a release** and **Sources for this series** could open off the bottom of a long page.

## [2.58.2] - 2026-11-07

### Fixed

- **A book's page fits the screen.** In Curl, Slide and None you never have to scroll up or down: the reader is pinned to the part of the screen you can see, stays clear of the phone's own bars, and makes the page again when the address bar comes or goes or you rotate the phone, on the same page. PDFs open with a whole page showing, and Fit width stays a button.

### Added

- **Scroll as a way to read.** In the reader's Aa settings, Reading is now Pages or Scroll. Scroll runs the book as one long column, with your place, bookmarks and highlights kept. Pages stays the default.

## [2.58.1] - 2026-11-07

### Fixed

- **Manga checks its folder before it fetches anything.** With a manga folder Usherr can't save to, downloads came down and then failed at the last step. Now it says so on the Manga settings page and doesn't search or download until the folder works. If your Docker setup has no manga folder yet, add one such as `/your/manga:/data/manga`.

## [2.58.0] - 2026-11-06

### Added

- **Manga, a section of its own** (off until you switch it on in Settings > Media types): type a series' English title and press Add, with no site to pick. Other titles work too, since the titles come from AniList. Choose All volumes, New volumes only or Nothing yet.
  - **Official English editions by default.** Usherr tells them from fan scans by the release's name. Fan scans and Nyaa are choices in Settings > Manga, off until you pick them, and a single series can differ. "Official" describes the edition, so a copy on an indexer still isn't a purchase: every series with a known publisher has a Buy it link.
  - **Every series says what's happening.** One sentence, when it looks next, and **Why nothing yet?**: "Searched 3 indexers for Volume 14. Found 4 results: 2 in French, 2 other volumes."
  - **Volumes arrive by themselves** when Usherr is sure, through your usual indexers, Activity, blocklist and limits. Chapters, other languages and spin-offs are turned down, and packs and anything doubtful wait for you. `Mob Psycho 100 v01` and `Kaiju No. 8 v01` now read as volume 1.
  - **Gentle on your indexers.** It searches once by the series' name, then by volume only if that found something, and waits longer each time nothing turns up: a day, three days, a week, then a month.
  - **Your own files count.** Usherr finds names like `Series/Series v01.cbz` in the manga folder, without moving or changing them, when you ask, when you add a series, and once a night between 2 and 9.
  - **A reader of its own.** Tap a volume to read it: right to left for manga, one long scroll for manhwa, your choice kept for each series. Two pages on a wide screen, one on a phone, and a two-page spread stays whole. Your place, bookmarks and a Continue reading row follow you, and the last page offers the next volume. CBZ, CBR, PDF and EPUBs made of pictures all read here.
  - **The book reader's page turn on manga.** The page curls after your finger or mouse, from the left edge when it reads right to left. Curl, Slide or None is the same choice as in the book reader, and a device asking for less motion just turns.
  - **Bring manga over from Comics** (Settings > Manga): you see every new file name first, files are moved and never deleted or overwritten, and Undo puts it all back. Where you'd got to and your bookmarks come too. Duplicates stay where they are and say why, and a series with no files comes with one tick.
  - **Nyaa is one switch.** Turning it on adds Nyaa to your indexers for Manga only, after a plain warning about seeding, and follows your VPN settings. Films and TV don't ask it. **Sources for this series** lets one series differ.
  - **Published editions as indexers really name them.** `VIZ.Media.Chainsaw.Man.Vol.21.2026.HYBRID.MANGA.eBook-21A1` and `Yen.Press-Yotsubaand.Vol.15...` were being turned down as another series, because the publisher comes first. They now count as Volume 21 and Volume 15 of an official edition. Searches ask the Books categories, so a title's anime doesn't crowd out its volumes.
  - **Odd formats are handled, not blocklisted.** A release holding only MOBI, or a novel in an EPUB, says so in Activity and asks you next time. When a pack holds a volume twice, the better copy is kept: official over fan scan, then pictures over PDF, then the bigger file.
  - In **OPDS**, so Panels, Chunky and KOReader read a page at a time, and **Keep on this device** works for volumes.
- **A page turn like paper in the ebook reader**: the page curls after your finger or mouse, with its back, the gloss on the bend and a soft shadow underneath. Pull it from the edge of the page (or anywhere sideways with a finger; hold still to pick words, as before), bring it back to change your mind, or flick it. Taps and arrow keys play the whole turn, in right-to-left books from the left. Text > Page turn: Curl, Slide or None; a system asking for less motion starts on None, and a browser that can't draw the curl slides.
- **A page turn like paper in the ebook reader**: the page curls after your finger or mouse, with its back, the gloss on the bend and a soft shadow underneath. Pull it from the edge of the page (or anywhere sideways with a finger; hold still to pick words, as before), bring it back to change your mind, or flick it. Taps and arrow keys play the whole turn, in right-to-left books from the left. Aa > Page turn: Curl, Slide or None; a system asking for less motion starts on None, and a browser that can't draw the curl slides.
- **The ebook reader fits how you like to read**: under Aa there is now line spacing, margins, justify on or off, more fonts (or the book's own) and a black page for OLED screens, all remembered. Tap a footnote mark to read the note in a small card and stay on your page.
- **Edit a book's details**: an Edit details button on the book's page lets you correct the title, author, series, number, year and description, use what the book's own file says, or match it again against Open Library, Hardcover and Google Books. No more deleting and adding it again. Files stay where they are.
- **Send a book to your Kindle or e-reader by email**: save your device's address on your profile, then press Send to e-reader on a book's page. Older Kindle formats go as EPUB, and EPUBs are mended so Amazon takes them. Up to 50 MB, through the mail server of your email notification. Add the sender to Amazon's approved list, or Amazon drops the book without a word.
- **Keep your place with KOReader and Readest**: Usherr is now a KOSync server, signing in with your app password. Your place follows between the apps and Usherr's reader, by page for PDFs and comics, and by percentage and chapter for EPUBs. See the guide for what can't convert.
- **NSFW 18+ tools** (NSFW page > Tools, for administrators; each runs only when you press its button):
  - **Match files by fingerprint**: finds video files in your NSFW folder that Usherr doesn't know and looks them up on StashDB. You see each file with the video it matched, how it matched and whether the length agrees. Nothing is added until you confirm, and files stay where they are.
  - **Bring in my StashDB favorites**: shows the favorite performers and studios of your StashDB account, then follows the ones you pick, from today on. No older videos are added and no search starts. It never unfollows anyone or changes anything on StashDB. A switch (off by default) repeats it every night, between 2 and 9.
  - **Rename and file**: shows old and new names as `Studio/Studio - YYYY-MM-DD - Title [quality].ext` before anything changes. You pick the videos; subtitles and `.nfo` files move along, nothing is ever overwritten, copied or deleted, and it can all be undone.
- **Audiobook player**:
  - **Bookmarks**: keep a spot with one press and add a note. They are listed in the player and on the book's page.
  - **Goes back a little after a pause**, more the longer it was paused. You can turn it down or off in the new player settings.
  - **Choose your skip lengths**, 5 to 60 seconds, back and forward.
  - **Lock screen**: phones that show a progress bar can now drag it, and the skips use your lengths.
- **Real audiobook lengths**: Usherr now reads how long each file really is, for a book when you open it and for the rest between 2 and 9 in the morning. A book in many files becomes one timeline, with a slider for the whole book, the time left, chapter start times and an exact percent, and skips carry on across files. Your saved places stay exactly where they were.
- **Better audiobook import**: reads the narrator, series number and Audible ASIN from folder names like `Vol 2 - Title {Narrator} [B0015T963C]`, the files `metadata.json`, `desc.txt`, `reader.txt` and `.opf`, and the tags in the audio. It only fills in what a book doesn't have, and files stay where they are. Importing again fills in the books you already have.

### Changed

- **Comics turn down more languages, and don't file a second copy.** Japanese and raw, Korean, Chinese, Russian and French `T25` releases are now refused, as German and French already were. A copy of an issue you already have isn't filed beside it any more: the same file is dropped, and one of another size is left out with a note in Activity.
- **The ebook reader**: a book now reopens on the page you left, not one page or spread earlier. Highlights sit on their words again, on the page and in the page curl, and show in the curl of the next chapter too. A page dragged while the screen drops the curl still turns. Someone who had chosen Fade now gets Slide. On an iPhone, words picked in a book can be highlighted again and links in a book work.
- **The virtual drive's own search keeps to the night** (Settings > Extras > Virtual drive): it runs only between the hours on the card, 2 AM to 9 AM to start with, so your streams have the Usenet connections during the day. The day's searches are spread evenly over those hours, so 60 a day is still 60. What you start yourself isn't held back. The card shows Usherr's clock and warns you if it's on UTC.
- **One missing article no longer ends a stream.** Usherr asks your servers once more, then plays past it as a moment's glitch, as long as the video can take it: at most 2 articles in a row, 5 in all and 1% of the file, and never in the file's start or in formats that can't recover. Past that, a title leaves the drive only once all your servers confirm the articles are gone, so a provider's bad moment can't take it out of Plex.
- **Season packs play the right episode.** Usherr now finds the episode's own file in a release by its name, also inside RAR archives. When a pack's names are scrambled it goes by the order and sizes only if everything adds up, and otherwise tries the next release rather than guess.
- **Streaming is quicker and holds up better.** Read-ahead grows when a player keeps catching up with it and shrinks when it doesn't, a jump goes straight to the new place, a hung connection costs about 3 seconds instead of a minute, and all streams together share 512 MB of memory.
- **Manga and webtoons in the Comics reader**: read right to left or left to right, one page, two pages, page turn, panels, or a **webtoon** view (one long scroll with no gaps). Keys, taps and swipes go the way the comic reads. Your choice is remembered for each series, and you can make it your default. A file marked as right to left, or a series set to manga, opens that way by itself.
- **Your reading in Comics**: a **Continue** row above your shelves, and a new **Reading** tab with Continue, Want to Read and Read, each kept for you alone. The last page offers the **next issue**. Mark an issue, everything up to it, or a whole series as read or unread. Reading to the last page marks it read for you.
- **Fix a wrong Comic Vine match** (a series' page > Fix match, for administrators): search again and choose the right volume. You see what it would do first. Issues are paired by number, so files, what people have read and bookmarks stay with them, and no file moves. You can also say what kind of series it is: comic, manga, webtoon or collected editions.

## [2.57.0] - 2026-11-06

### Added

- **Tidy up your music** (Music page > Tidy up, for administrators): Usherr checks your music folder and shows the new folder and file names, tags, covers and any albums you have twice, before anything changes. You pick the albums. Originals are kept for 30 days and it can all be undone. Add a free AcoustID key and it also recognises albums by their sound.

### Changed

- **Music downloads are matched more carefully** (following DroppedNeedle):
  - A file whose title tag names another song is no longer filed as a track just because its number and length fit (a live or bonus track in the wrong place). Files without a title tag are matched as before.
  - When a download is another edition of the album (a deluxe with bonus tracks, or a shorter original), Usherr switches to the edition with that many tracks, so the extra songs are imported instead of left out. It only does this while none of the album is in your library yet.
  - The scheduled search looks for new albums more often and old ones less: every 2 days in an album's first month, then every 4, 7 and finally 14 days.

### Security

- The torrent engine is updated past a flaw in how it read torrent files (GO-2026-6894).
- The PDF reader is updated past a flaw that let a malicious PDF run code when it was opened (GHSA-hq66-cqwq-w95j).

## [2.56.0] - 2026-11-06

### Changed

- **Built on Mediarium 3.9.4** (from 3.9.0). What's new from it:
  - **Book search** also asks Hardcover (when a token is saved) and Google Books, for missing covers and descriptions and for books Open Library doesn't list.
  - **Titles and posters in your info language** across the library, Activity, the calendar and requests, filled in by a background job.
  - **UDP trackers and DHT through the VPN** (Settings, Downloading, VPN and proxy; also a new step in setup, before Extras). Tracker names are looked up through the VPN too.
  - **Search for other words** when choosing a release for a film, season or episode.
  - Fewer false "too many connections" warnings from Usenet, SABnzbd and NZBGet jobs cleaned up after import, albums no longer linked to a single of the same name, and many search, naming and download fixes. See [CHANGELOG.md](CHANGELOG.md) 3.9.1 to 3.9.4.
- Pausing a download while its files are being repaired now stops the repair at once.

## [2.55.0] - 2026-11-06

### Added

- **Music:**
  - **Bring in playlists** (Settings, Playlists). The .m3u and .m3u8 files in your music folder become playlists, with a list of the tracks that weren't found. Usherr can check them again each day (off by default), and any playlist can be saved as .m3u8.
  - **Last.fm.** An administrator enters a Last.fm API key once (Settings, Services, Last.fm), then everyone can connect their own account. What you play goes to Last.fm and ListenBrainz; plays made while they can't be reached are sent later, and the new heart in the player can love the track on Last.fm too.
  - **Phone apps** (Settings, Services, Phone apps; off until you switch it on). Symfonium, DSub, Substreamer, play:Sub, Feishin and other Subsonic and OpenSubsonic apps can play your music from Usherr. They sign in with your user name and your app password (the same one e-readers use).
  - **Streaming quality.** Phones on mobile data can get smaller files (Opus, AAC or MP3), set per device in the player; files that already fit play as they are.
  - **Lyrics found online can be saved** as .lrc files next to your music (off by default; an existing .lrc is never replaced).
  - **Safer album downloads.** Karaoke, tribute, live and bootleg releases are left out unless that's what you asked for, as are releases too small to be the album and, on import, files of the wrong length.
  - Thanks to DroppedNeedle (AGPL-3.0), whose approach several of these follow.
- **Books and comics:**
  - **Comics in reading apps.** Your comics are in Usherr's OPDS catalogue by series, with covers; Chunky, Panels and KOReader can read them a page at a time without downloading the whole issue, and pick up where you left off in Usherr. One search finds books and comics.
  - **PDFs in Usherr.** PDFs open in Usherr's own reader, page by page, and remember your place.
  - **Bookmarks and highlights.** Highlight words in four colours, add notes, and bookmark pages in the ebook, PDF and comic readers. They're yours alone, listed on the book's or series' page, and jump straight back.
  - **Read offline.** "Keep on this device" saves a book, comic or audiobook to read with no connection (over https); your place is sent when you're back online. See and free the space under "On this device"; signing out clears it.

### Changed

- The e-reader password is now called the **app password**: one password for reading apps and music apps, never your sign-in password.

## [2.54.0] - 2026-11-06

### Added

- **Watch together with linked Usherrs** (Showtime). A linked screening now plays in step at every house. When someone at a guest's Usherr says I'm in, the screening is booked in its own Showtime, hosted by your Usherr, so the Marquee counts down to it; moves and cancellations follow, and it goes when nobody there is coming. Your own household is listed first among who's coming, and anyone in it can say I'm in or Can't make it. Shortly before the pre-show, your Usherr sends what's in it, piece by piece; each guest plays the same pre-show with its own cards, clips and trailers, and anything it doesn't have is a card of the same length, so the film starts at the same moment everywhere. When it's time, press "Join the screening" on the Showtime page; joining late starts where everyone has got to. The host's play, pause and skips go to everyone, and every player stays within a fraction of a second, speeding up or slowing down slightly, or jumping when it's further off. Guests see "Following the host", how far off they are, and "Host paused". "Anyone can pause" (off by default) lets guests ask to pause. If the relay drops, the film plays on and gets back in step when it's back. This follows in Usherr's own player; Jellyfin and Plex don't follow yet.
- **Sports downloads, without Sportarr.** Usherr's own sports engine now downloads games. Each league chooses whether it gets its new games, which parts of a fight card or sessions of a race weekend, only the teams you follow or every game, and in which quality profile; followed teams can get their games too. Usherr looks for each game on your indexers a few hours after it ends, then less and less often for two weeks, within your indexers' daily limits. On a played game, Search now and Pick a release show every release, with why the others aren't that game. Games download through your usual download clients and are filed in a Sports folder the way Plex and Jellyfin read a TV library (League / Season 2026 / League - S2026E05 - Game), with NFO files, and the media servers are told to scan. Wanted games show on the Sports page and the Calendar, and their downloads in Activity under the game's name. Settings, Extras, Sports has the Sports folder, the quality for games and the automatic search, plus "Bring my followed leagues from Sportarr" (it only reads Sportarr, and brings no profiles).

### Fixed

- An invite code with one wrong character is always caught as a typo. Before, a few typos slipped through and were reported as "That code doesn't work anymore". Codes made by 2.52 or 2.53 that haven't been used yet may now read as a typo: make a new one.

## [2.53.0] - 2026-11-06

### Added

- **Linked screenings** (Showtime; needs Linked Usherrs switched on). Share a showtime with linked Usherrs: press Invite on a booking and tick who to invite. A linked Usherr only sees screenings it's invited to. They see it in their own Showtime under "From linked Usherrs" with your name, the film, the start time and the pre-show length, and their people answer I'm in, Can't make it or Maybe. You see who's coming from each house, how many, and whether they have the film; everyone at the screening sees who's coming. Their Usherr checks its library, saved streams, downloads and requests for the film: if it isn't there, "Get it before showtime" asks for it, and if their copy runs more than two minutes longer or shorter than yours, they're told it may be a different cut. Moving or calling off a showtime follows to every invited Usherr, taking someone off the list tells them, and unlinking removes shared screenings on both sides. A new notification kind, "Linked screenings", covers invites, moves and cancellations. Each screening has its own key, and the relay hands one message to everyone at a screening at once, so a screening can have 50 or more linked Usherrs.

### Fixed

- On a phone, the episode list on a show's page no longer pushes the page a few pixels wider than the screen.

## [2.52.0] - 2026-11-05

### Added

- **Linked Usherrs** (Settings, Movie night, Linked Usherrs; off until you switch on "Link with other Usherrs"). The first step of watching together with friends who run their own Usherr, without Discord. "Invite a friend's Usherr" makes a one-time code (USH-XXXX-XXXX-XXXX-XXXX-XXXX-X, works once, for 24 hours, can be cancelled); your friend pastes it under "Join with a code" and the two Usherrs are linked. A mistyped, used, cancelled or expired code says so. Each link shows its name, its fingerprint (to compare if you want to be sure), when it was last heard from, Rename (here only) and Unlink, which tells the other Usherr and stops the link on both sides at once. Linked Usherrs talk through the Usherr Link Relay, a postman run by the Usherr project at relay.usherr.app: every Usherr keeps one outgoing connection, so no port is opened and nobody sees your home address, and everything is encrypted and signed end to end, so the relay can't read any of it. The page shows whether the relay is connected and warns when the server's clock is off. "My own relay" lets you use a relay you run yourself (the small usherr-relay program; codes then carry its address). Screenings, synced playback, chat and rewards over the link come next. This Usherr's keys are kept in link-identity.key in the settings folder, not in the database or its backups.

## [2.51.0] - 2026-11-04

### Added

- **Collections** (Settings, Re-Posterr, Collections). Usherr makes collections in Jellyfin and Plex and keeps them up to date, so Kometa isn't needed for them. A collection's titles come from one of your import lists (Trakt, MDBList, TMDB lists, IMDb, Letterboxd), a TMDB collection (a film series, by name or number), TMDB's trending titles, a rule over your library (genres, years, a minimum rating, added in the last N days, or just the best rated few) or titles you pick. Titles are found in each server's library by their TMDB, IMDb or TheTVDB id; Jellyfin gets one collection (the kept copy of a film rather than its streamed one), and each Plex library with matching titles gets its own. Choose Jellyfin, Plex or both, a sort title, a poster (the server's own, one you upload, or one made in your Re-Posterr style), where Plex shows it (the library's Recommended tab, your Home, friends' Home), whether titles that no longer belong come out, and whether missing titles are added to Usherr as wanted. "See what's in it" shows the matches and what's missing before you save. Collections sync every 12 hours (or what you choose) and on "Sync now"; one of the same name already on a server, say one Kometa made, is taken over instead of made twice. Removing a collection asks whether to delete it from the servers too (the titles are never touched).
- **Poster badges** (Settings, Re-Posterr, Poster badges; off until you switch them on). Small badges on the posters Jellyfin and Plex show: 4K, 1080p or 720p, HDR, HDR10+ or Dolby Vision, the best sound (Atmos, DTS:X, TrueHD, DTS-HD MA), 5.1 or 7.1, the rating (your media server's, or IMDb's from MDBList scores) and "New" for titles added lately. Choose which badges, the libraries that get them, the corners, a dark or colourful look and the size, with a live preview of one of your films. Usherr keeps every original poster and always draws on it, so badges never pile up; posters are drawn again when a file or a rating changes, and switching badges off (or unticking a library) puts the originals back. A poster changed another way, by hand or by Re-Posterr, becomes the new original, and Re-Posterr keeps the poster without badges when it replaces one. Picture and sound badges are for films; shows get the rating and New.

## [2.50.0] - 2026-11-04

### Added

- **Clean-up rule sets** (Settings, Media servers, under What's been watched), so Maintainerr isn't needed. Make a rule set for the movies, the TV shows, or one library folder, with conditions joined by "all" or "any": watched more than so many days ago, never watched, nobody watched it lately, watched completely, files bigger than so many GB, released before a year, has or doesn't have a tag, added or requested by someone, nobody requested it, quality, and the show has ended. Simple shows the three common rules; Advanced shows every condition. For shows, pick whether a rule acts on each episode, a whole season once it's watched, or the whole show. Then pick what happens: delete the files and stop looking for the title (the default), only stop looking, or delete and download again later. The recycle bin applies when it's on.
  - **Leaving soon** (Upcoming, Leaving soon): a title that matches goes on this list with a date first (14 days unless you change it), and is only acted on when that date comes, if it still matches then. Everyone can see the list. Administrators can let members press "Keep it", which keeps the movie or show for good; who kept it is shown and written to Activity. Leaving soon can also show as a collection on Plex, Jellyfin or Emby (a switch for each server, off unless you switch it on).
  - Safe by default: every rule set is off until you switch it on, Preview lists exactly what would go on Leaving soon and what would be acted on now, with the reason for each; one run acts on at most 50 titles (a setting); titles with a keep tag and kept titles are never touched; streamed titles (.strm files, the Stream folders and links into the streaming drive) are never touched; nothing is done unless what's been watched was read from every media server in the last two days; and every action is listed under Activity. The basic cleanup rules keep working as they did.

## [2.49.0] - 2026-11-04

### Added

- **Watching** (Settings, System, Watching): Usherr keeps its own watch history from Plex, Jellyfin and Emby, so a separate Streamystats is no longer needed. It asks the servers what is playing every half minute (every minute when nothing is) and keeps each playback: who (matched to their Usherr account when they sign in with Jellyfin or Plex, or have the same name), the film or episode, the app and device, at home or away, direct play, direct stream or transcode and why, the picture and sound before and after, how long it was actually watched (pauses don't count) and how far it got (90% counts as finished). Three tabs: Now playing (live, with progress, how it plays and the bandwidth), History (filter by person, server, title, device, how it played and dates) and Stats for 7, 30, 90 days or a year (hours per day, per person, top movies and shows, apps, devices, how much played directly and the most common reasons for transcoding, and the busiest hours and days). A film's or show's page adds who watched it to its "Watched" line. Members see only their own history unless you let them see everyone's. Settings: keep the history for a year by default (or 30 days to forever; older playbacks go once a day), and players' IP addresses are only kept if you switch that on, with a button to clear them.

## [2.48.0] - 2026-11-04

### Added

- **Live TV picture** (Settings, Extras): choose the video encoder for Live TV channels: Automatic (the default), CPU (libx264), NVIDIA (NVENC), Intel Quick Sync or VAAPI. Automatic tries each with a one-second test and uses the first that works, in that order; the card shows which work and, for the others, why not, with a Test again button. If an encoder fails while a channel plays, the CPU takes over for that program. The encoder is used when a stream has to be converted for a web browser and when the watermark is on.
- A watermark for Live TV channels, off by default: each channel's logo, a picture you upload (PNG, JPG or WebP), or the channel number or name, in the corner you pick, with its size, opacity and distance from the edge. A preview shows it on what a channel is playing now, and each channel can be left without it. With the watermark off, channels still send the picture as it is, as before. This does the job of ErsatzTV's watermark.

### Changed

- The Docker image is now built on Debian instead of Alpine, with Jellyfin's ffmpeg build, so Usherr can encode video on the graphics card: NVIDIA (NVENC) as well as Intel Quick Sync and VAAPI. To use an NVIDIA card, give the container the GPU in compose (`gpus: all`, or a device reservation). Everything else works as before; the image is somewhat larger.

## [2.47.0] - 2026-11-04

### Added

- "Ask for more seasons" on a show's page (About tab). Anyone who isn't an administrator sees it when the show has seasons that aren't downloaded or wanted yet: pick seasons and they're asked for (or switched on and searched straight away, for an account that may add without asking). Administrators keep switching seasons on in the episode list.

## [2.46.0] - 2026-11-04

### Added

- **Service checks** (Settings, System, Service checks; also a tile in the Server area). Usherr asks the addresses you list every so often (Jellyfin, Plex, your download clients, Prowlarr, or any web address) and shows whether each is answering, since when, its response time, the latest checks as a strip, uptime over the last 24 hours and 7 days, and its outages. For each: what counts as working (200 only, any 2xx or 3xx, or any answer), an optional word the page must contain, how often to check, and how many failed checks in a row before a message (3 by default, so a restart or a blip stays quiet). "Try it" tests an address before you save it, and Usherr suggests the media servers, download clients, Prowlarr, FlareSolverr and add-ons it already knows. Two new notification events, "Service down" and "Service back", send one message when a service stops answering and one when it is back; tick them on a notification to get them. This does the job of a separate watchdog container.

## [2.45.0] - 2026-11-04

### Added

- Usherr plays saved NZBs again by itself, so StreamNZB isn't needed for them. A streamed movie or episode with a saved release is read straight from your Usenet servers: no search, and no NZB fetched from an indexer again. Before a saved release is offered, Usherr checks a sample of its articles on your servers (the answer is kept for 12 hours); one whose articles are gone is marked dead and the drive's search looks for another. When your first Usenet server is missing an article, the next one is asked, and Usherr never opens more connections than each server allows.
  - The streamed library (Jellyfin's pointer files) plays the saved release first when Usherr's own engine is chosen (Settings, Extras, Streaming). With StreamNZB chosen, the saved release is still the last resort, now played by Usherr.
  - The Stremio and Nuvio add-on offers a title's saved release as a "Saved" stream, above the releases found by search, so a Stremio install can point at Usherr's add-on address instead of StreamNZB's.

## [2.44.0] - 2026-11-04

### Added

- Ask for some seasons of a show, as in Seerr. The add (or request) dialog lists the show's seasons: pick some and only those are monitored and searched for. Asking for more seasons of a show that's already here switches those seasons on and searches them; each set of seasons is its own request.
- Friends sign in with the Jellyfin or Plex account they already have (Settings, Account & Access, Ways to sign in, "Sign in with Jellyfin or Plex"; off until switched on). Jellyfin names and passwords are checked by your Jellyfin server; "Sign in with Plex" lets in Plex accounts your Plex server is shared with. The first sign-in makes a member (or viewer) account, found again later by the Jellyfin or Plex account's own id. Names with a password here keep signing in here, and two-step sign-in still applies.

### Fixed

- The list of routes members may use now includes removing and restoring a ticket stub (2.41.0), so the routes check passes again.

## [2.43.0] - 2026-11-04

### Added

- Tags that do something, as in Radarr and Sonarr (Settings, Library, the automation page, "Tags").
  - Auto-tagging: a rule gives its tag to every movie or show that matches it, by genre, years, the language it was made in, movie or show, and library folder, and can take the tag off titles that stop matching. Rules run when saved, from "Tag the library now", and every six hours for new titles. Tags still become collections in Plex, Jellyfin and Emby.
  - Tag rules: titles with a tag search only the indexers ticked for it, or only Usenet or torrents, in automatic searches. With a profile that limits indexers too, only indexers both allow are used.

## [2.42.0] - 2026-11-04

### Added

- Quality profiles have a Simple and an Advanced mode (Settings, Quality, the switch beside "Quality profiles"; it replaces the "Show advanced options" switch).
  - Simple: pick a quality (Save space, HD, Best HD, 4K, or 4K else HD), set the largest file with a slider or by typing any size up to 1000 GB (or no limit), and three switches. A list shows which example releases the profile would take, and why. Settings made in Advanced are kept and named.
  - Advanced: everything a profile had, plus sizes per quality (smallest, preferred and largest), the smallest score gain worth an upgrade, stopping upgrades a number of days after release, TRaSH score sets added in one step, preferred and never-taken release groups, patterns between slashes in the release-name rules (like /AMZN|NF/), the profile's own audio languages, preferring English subtitles or x265, which indexers it searches, Usenet or torrents only, release age limits, and Export as JSON.
  - Both: "Try a release name" judges any release name against the profile as it stands in the form, with the same checks a search makes.
- Bring your custom formats over from Radarr and Sonarr (Settings, Import, "Custom formats"), ready to score in Usherr's own profiles. Their profiles stay behind. Importing again updates the copies instead of adding more. A format both apps have under one name keeps the two apart as "<name> (TV)".

## [2.41.0] - 2026-11-04

### Added

- A friend's watch party, joined with its showtime code, now links up with the friend's Discord thread. Once your Usherr's own bot is in their server (they add it with your bot's invite link), Usherr finds the party by its code by itself. You see and join its chat on the Showtime page. Your ticket stub says who hosted and who else came. When you press the host's "Get my stub stamped", your stub here is stamped too. The Showtime page walks you through the steps and has a button to copy the invite link; so does Settings, Movie night, Community. (GitHub issue #2)
- Remove a ticket stub (You, Ticket stubs). You confirm it twice, and Undo brings it back. (GitHub issue #3)

## [2.40.0] - 2026-11-04

### Added

- **A new layout to try: Neu, new layout.** Settings → General → Look. It's Neu exactly, arranged differently: your libraries always in sight down the side, tabs along the bottom of a phone, a Home page for watching (the next showtime, Continue watching, Just added, Coming soon) and one Server area for admins with what needs you, the day-to-day pages and every settings group. A TV size makes everything bigger for the sofa, and turns on by itself in a TV's own browser. Kept per browser; Neu and Classic are unchanged.
- **Film and show pages in the new layout.** The title over its own backdrop with one button to press, then the episodes, About, you and your friends, the cast and more like this. Members see a page for watching: no quality, file sizes, monitoring or Manage, and a film or episode that isn't here says "On its way" or "Not here yet". A show's episodes come one season at a time, each with Play, Resume and the minutes left, or the day it arrives. A film you don't have says "Ask for it" (or "Add it" for those allowed to add straight away). Admins get Manage beside the page instead of under it, and Episode tools for the full episode table. Members get "Tell an admin" for a problem with a title.
- **Continue watching on the Dashboard.** The films and episodes you stopped part way through in Usherr's player, newest first, with how much is left. Each opens its page, where Play picks up where you left off. Only your own, never someone else's.

### Fixed

- **Fixes from the AI testers' second round: wording, phones and screen readers.**
  - A showtime code that doesn't work says why: too short or too long (with how many letters it has), or a typo.
  - The help guide's own section is called "Usherr's own", not "Home fork".
  - A few places still said Mediarium (the version's tooltip, the downloads list, the search box); the Support button says Mediarium is the project Usherr is built on.
  - Members no longer see the note about setting TZ in the compose file.
  - The Stremio and Nuvio card sits at the end of You and says it's only for those apps.
  - On the downloads page: "1 Problem", not "1 Problems", and no "Blocklist ()".
  - On a phone, the Book a showtime dialog fits the screen with room around its contents, the profile menu scrolls instead of cutting off Sign out, the "menu slides away" setting no longer squeezes to one letter a line, and a long status on a poster ends in dots instead of breaking in half.
  - The −15 and +15 buttons on a showtime say "min", and a title's quality, source, upgrade and season-pack menus have clean names for screen readers.
- **Plex's Live TV tuner no longer gives away the key to your streamed films.** Plex reads the tuner's channel list without signing in, so anyone on the network could read the key in it, and that key also played every streamed film and episode. Live TV now has a key of its own that opens Live TV only; the streamed films' pointer files keep theirs, so nothing needs rewriting, and Plex and Jellyfin keep working as they are. Keys are now compared in constant time.
- **The Showtime page's Book a film search says why it found nothing.** It showed an empty box both when there were no matches and when the search failed (with no TMDB key yet, say). It now says "No films found" or what went wrong.
- **A show's episodes fit a phone.** The everyday list (without Episode tools) no longer scrolls sideways off the screen: Play is always in sight, the air date sits under the title, and the status no longer breaks in half.
- **A showtime days away counts down in days.** "Starts in 4 days" or "Starts tomorrow", not "Starts in 117 h 59 min". In the top bar a showtime this week shows as "Sat 7:30 PM", short enough for a phone, so it no longer pushes your profile button off the screen.

## [2.39.0] - 2026-11-02

### Added

- **Ticket stubs for a friend's watch party, in your own Usherr.** Join a friend's party with its showtime code (Showtime, Join a watch party) and your own Usherr hands you a stub at the door when the film starts, with nothing to link: a friend's bot can't reach your Usherr. It's stamped by itself when Usherr's player reaches the credits; if the film played on the TV or the host ended it early, **Stamp it** on the stub works for half an hour after the film's end. The empty Ticket stubs page now says all of this instead of only pointing at linking.

### Fixed

- **`/link` says who it's for.** Its code works only in the Usherr the bot belongs to; someone running their own Usherr is told they don't need it (I'm in, then the showtime code).
- **The update notice says how to update Usherr.** It called every Usherr "your home build of Mediarium" and gave no way to update. It now says to pull the new image and restart it, or build the new release if you build Usherr yourself.

## [2.38.0] - 2026-11-01

### Changed

- **A film's page, cleaned up.** It fits on one screen: the title, three lines of the plot (More shows the rest), and one row of everyday buttons (Play, Showtime, Stack, your thumbs, Manage). Everything about the file and its downloads moved under **Manage**. Three layouts to pick from in **Settings → General → Film and show pages**, kept per browser: **Cinema** (tabs), **Side by side** (you and the reviews on the right) and **Streaming** (a big backdrop and rows, for the TV), or **Automatic** by the screen. **Show on the page** switches the scores, where it streams, your watches, reviews, the cast, More like this and the whole plot on or off. *Show me how* has *Choose how a film's or show's page looks*.
- **A show's page, cleaned up the same way** (anime too). The same three layouts and switches, with **Episodes** as the first tab in Cinema. Everyday buttons up top (Stack, your thumbs, Catch me up, What's it like?, Manage); Find missing episodes, Refresh, Rescan, Remove, Monitored, the profile, the series type, the folder, episode numbering, copies, What happened and the files are under **Manage**. The episode list keeps to watching, and **Episode tools** adds monitoring, searching, quality, size and each episode's file buttons. A show not yet in your library gets the layouts too, and both now have **More like this**.
- **The heart button says a word first.** Before Ryan's Ko-fi page, it explains that Usherr is built on his Mediarium and couldn't exist without it, asks you to share Usherr so it can grow (Share Usherr copies its link, or opens your phone's share sheet), and says feedback and bug reports are welcome. Its tooltip now says Support Mediarium, not Support Usherr, and the GitHub button says Star Usherr.
- **Sort and narrow a person's work.** On an actor's or director's page, a compact row of menus replaces the All / Acting / Directing bar: **Find a title**, the role, movies or shows, the genre, whether it's in your library, and the order (most popular, newest, oldest, highest rated, A to Z). The choices stay in the address, so Back returns to them. TMDB has no awards, so there is no awards filter.

### Fixed

- *Show me how* walkthroughs that point at something under Manage on a film's page (*Move a film to another folder*, *Make a Kids shelf*, *Re-Posterr*) open Manage first.
- **Report a problem: the support report gets pasted, not a note.** The form used to fill GitHub's Support report box with "Paste the support report here", and that note was what got sent. The box now starts empty, and both Usherr and the GitHub form say to click in it and paste. If Usherr couldn't copy the report, it says so.
- **The web app's own tests run again.** One file's import (`./brand`, without `.ts`) stopped 48 of the test files from loading.
- **The GitHub forms say Usherr.** The bug, idea and indexer forms asked for the "Mediarium version" and pointed at Mediarium's pages, Discussions and Reddit. They now ask for the Usherr version, give Usherr's menu paths, and link to Usherr's own security, install and features pages.

## [2.37.0] - 2026-11-01

### Added

- **Community (preview): Usherr in your own Discord server.** Watch parties, a chat, reviews and ticket stubs with your friends, through your own Discord bot. Settings, Movie night, Community connects it: make the bot once in Discord's Developer Portal, paste its token (kept encrypted, never shown again), invite it with the link the page gives, and choose where it posts. It connects from your home to Discord, so nothing on your network is opened to the internet, and nothing it posts can ping anyone. Moving to another server later is only a setting. *Show me how* has *Connect Usherr to your Discord server*.
- **Link your Discord account.** `/link` in your server answers with a code only you see; type it in Settings, Linked services, under Discord. Linking posts nothing, Unlink undoes it, and administrators see who is linked. *Show me how* has *Link your Discord account*.
- **Watch parties.** Tick *Share to Discord as a watch party* when you book a Showtime (or press Share on the Showtime page). Usherr posts a card with the poster, the times in each reader's own time zone, a countdown, who's going and I'm in / Can't make it buttons; opens a thread for the chat; and puts the party on the server's calendar. The thread is told when the pre-show and the film start; moving the Showtime moves the card, the event and the thread with it, and cancelling it calls the party off (or ends it, during the film). The Showtime page shows who's going.
- **Join a watch party from anyone's Usherr.** Each party's card has a showtime code like HXK4-7QPM-2RD9C. Pasted under *Join a watch party* on the Showtime page, it books the same film for the same moment, with a pre-show of the same length made of that Usherr's own clips and trailers; the two Usherrs never connect. A mistyped code is refused rather than booking the wrong film. *Show me how* has *Join a watch party from Discord*.
- **The watch party chat in Usherr.** The party's thread shows live on the Showtime page under the countdown, with pictures, GIFs and spoilers hidden until tapped, and on a page of its own for your phone while the film plays on the TV. What you type is posted in the thread under your own Discord name and picture. Over the film, **Chat** on Usherr's player fades each new message in at a corner of the picture (off until switched on; full screen keeps it). *Show me how* has *Chat during a watch party*.
- **Reviews.** One review each per film or show, stars and a few words: from a film's page (*From the community*) or a Diary entry in Usherr, with `/review` in Discord (the film suggested as you type; a form asks for the stars and words), or as `#review 4.5/5 …` in a watch party's thread. **Reviews go to** picks a text channel or a forum; in a forum each film gets its own post with every review of it as a reply, a party's #review is copied in with a link back, and other replies are just conversation. Posting again replaces your review, Remove works only for its writer, and every film's page shows its reviews. *Show me how* has *Review a film for your Discord*.
- **Ticket stubs.** Like a cinema's: when a party's film starts, everyone who pressed I'm in (and the host) gets a stub at the door, with a seat in the order they said so; at the credits, **Get my stub stamped** in the thread (30 minutes) stamps it *Stayed to the end*. They show as tickets under **You → Ticket stubs**, and `/stubs` lists them in Discord. *Show me how* has *Collect ticket stubs*.

### Fixed

- **Book a film on the Showtime page**: the search results' posters spilled over each other, a poster stored at a bigger size the most. Each now fits its place in the grid.

## [2.36.2] - 2026-11-01

### Changed

- **Show me how, Let people reset a forgotten password** now walks the whole way: a Gmail app password, the email notification and its test, the address you open Usherr at, your account's email address, the switch, and a try.
- **Gmail app passwords: type them without spaces.** Google shows one in four groups, but with the spaces Gmail refuses it; the Password help, the 534 explanation and the docs say so.
- The address setting (Settings, General) and the email form's From name say Usherr instead of Mediarium.

### Fixed

- **Forgot password?**: pressing Enter in its box sent the link and also tried to sign in, showing "Enter your username." Enter now only sends the link.
- **The reset email** said a request came "from the address 172.19.0.1" (Docker Desktop's own address for every device at home); it now says "from a device on your home network", and names the address only from outside it.

## [2.36.1] - 2026-11-01

### Added

- **Forgot password? by email**: when an administrator switches it on (Settings, Account & Access, Ways to sign in), Forgot password? on the sign-in page emails a link to set a new password to the address on the account, administrators' too. It works once, for 30 minutes, and signs the account out everywhere; two-step sign-in still asks for its code. It goes through the mail server of your email notification, and the link starts with the address you open Usherr at, never one a request names. The reply never says whether an account exists, only a scrambled copy of each link is kept, requests are limited (about 5 from one address in 15 minutes, 3 emails an hour to one account), and every request and reset goes on Activity. The command line stays as the last resort. *Show me how* has *Let people reset a forgotten password*.

### Fixed

- **The pre-show before a streamed film in the browser**: with *Also before any film I play in this browser* on, a film from your library got its pre-show in Usherr’s player, but a streamed one (the Stream button, the Streamed page, the streamed libraries) went straight to the film. Now both get it. A film tried for under a minute starts from the beginning again, with its pre-show, instead of picking up those few seconds; and a Showtime’s streamed film doesn’t get a second pre-show.

## [2.36.0] - 2026-11-01

### Added

- **A 1080p H.264 copy of 4K trailers**: a downloaded trailer above 1080p (or not in H.264) also gets a 1080p H.264 copy beside it, for phones and laptops; trailers already downloaded get theirs a couple at a time.

### Fixed

- **The pre-show in a phone's browser**: it played in the Jellyfin app and on a PC, but a phone's Chrome skipped the clips and trailers it can't decode (a 4K VP9 trailer, a Dolby Vision clip in MKV with Atmos), since the browser got every file as it was. Now Usherr checks each clip and trailer against what the browser and screen play, as the film player does: it plays the file as it is, a trailer's 1080p copy, or a copy converted as it plays (1080p at most), and one that won't play is played converted instead of left out. A phone that won't start a clip with its sound by itself shows **Tap to start** instead of a black screen.

## [2.35.0] - 2026-11-01

### Added

- **Plex converts for Usherr's player**: when a browser can't play a file from your library as it is (HEVC, HDR, Dolby, DTS or TrueHD sound) and no Jellyfin or Emby converts it, Plex does, before Usherr's own conversion on the processor. With Plex Pass and *Use hardware acceleration when available* ticked in Plex, it uses the graphics card (Plex's dashboard shows *Transcode (hw)*). Usherr passes Plex's stream on, so the browser never sees Plex or its token, and closing the player stops it. The player says who converted it. Another sound track than the first, or a burned-in picture subtitle, is still converted by Jellyfin or Usherr.
- **Converting for the player: choose who goes first** (Settings, Extras): whichever is connected (Jellyfin or Emby, then Plex), Jellyfin, Plex, or Usherr itself. Whichever can't convert a file hands on to the next, and Usherr is always last.

### Changed

- The *Jellyfin or Emby* card no longer says Plex can't get streamed titles or Live TV: it gets them through the virtual drive and Add to Plex.

### Fixed

- **Live TV in Plex, every channel on Unknown Airing**: the guide says what to do when it lasts (restart Plex, which reads the guide again).

## [2.34.0] - 2026-10-31

### Added

- **Virtual drive: Plex runs on this computer, or another one**: with Plex on the computer Usherr runs on, the drive's rclone commands and startup file reach Usherr at 127.0.0.1, which never changes, instead of the computer's address on your network, which a restart can change (Plex then said every streamed title was unavailable). Usherr works it out from Plex's address until it's chosen on the drive card. For Plex on another computer, the card and *Set up Plex with the virtual drive* say how to reserve the address in the router.

### Fixed

- **Something not right? sees a drive Plex can't reach**: when Plex is asked to look at a new title on the drive and nothing comes to read it within 15 minutes, it warns that Plex can't reach the drive and what to do. With Plex connected, a drive nobody has read for a day is now a warning too (it was a note after two days).
- **Usherr's address on your network** is called that everywhere, instead of Mediarium's.
- **The guide**: Usherr's own pages say Usherr instead of Mediarium, the streaming engine instead of only StreamNZB, Add to Plex for Live TV, Re-Posterr with Plex, the virtual drive in *How the parts fit together*, how the two address settings differ, and what to do when Plex says a streamed title is unavailable.

## [2.33.6] - 2026-10-31

### Fixed

- **Add to Plex, pressed again**: with Usherr's channels on in Plex already, it only has Plex read the guide again, instead of sending the channel map again (which Plex can take minutes over).

## [2.33.5] - 2026-10-31

### Fixed

- **Add to Plex**: the guide's channels are read where Plex puts them (inside its lineup), so the tuner's channels can be matched and switched on.

## [2.33.4] - 2026-10-31

### Fixed

- **Add to Plex switches the channels on**: Plex's own setup links each tuner channel to its guide channel and switches it on; Add to Plex didn't, so Plex had the channels but none on, and no airings. It does now, also when pressed again on a DVR made before.

## [2.33.3] - 2026-10-31

### Fixed

- **Plex: a button always tries**: after Plex didn't answer once, Usherr left it alone for five minutes, buttons too (Add to Plex said to connect Plex first). Now only background work waits, and for two minutes.

## [2.33.2] - 2026-10-31

### Fixed

- **Add to Plex**: Plex answers some Live TV calls in XML, or with nothing, which Usherr took for something that isn't Plex. Both are read now (the new DVR is looked up when Plex doesn't say it), and an error says which step it came from.

## [2.33.1] - 2026-10-31

### Fixed

- **Live TV in Plex: the guide matches the channels**: Plex matches its guide to its tuner's channels by their id, and Usherr's guide named them as Jellyfin does, so Plex enabled none and had no airings. Plex now gets a guide of its own (plex.xml) naming each channel by its number, as the tuner does; Add to Plex, pressed again, makes Usherr's DVR again with it on the same tuner.

## [2.33.0] - 2026-10-31

### Added

- **Live TV in Plex, set up by itself**: *Add to Plex* on the Live TV page (beside Add to Jellyfin) adds Usherr to Plex's Live TV & DVR (Plex Pass): Usherr answers as an HDHomeRun tuner at its own address, Plex makes a DVR of it with Usherr's guide and reads it. The tuner's addresses answer only once it's been pressed. Channels relayed from a live HLS stream stay out of Plex's lineup.

## [2.32.0] - 2026-10-31

### Added

- **The pre-show in the Plex app**: Usherr puts a pre-show together for Plex every half hour (the theme's clips, the trailers downloaded, the cards; not the rating card, as Plex doesn't say which film is next), puts it in the virtual drive's Preshow folder and points Plex's pre-roll at it (Plex Pass). A pre-roll from NeXroll or set by hand is left alone. Like Jellyfin's, Plex's pre-show takes the same theme as everywhere or one of its own, so each server can have the same pre-show or a different one.

## [2.31.0] - 2026-10-31

### Added

- **Re-Posterr: the same pictures on both servers, or different ones**: with Jellyfin and Plex both, pictures go to both by default. *Different pictures for each media server* lets a picture chosen by hand go to Jellyfin, Plex or both, and gives Your style a style for each server as a whole and for each of Plex's libraries (a copy takes its library's style, else its server's, else the house style).

## [2.30.0] - 2026-10-31

### Added

- **Re-Posterr with Plex**: Re-Posterr picks and makes posters and backgrounds for Plex's films and shows too, in a Plex-only household or beside Jellyfin. Pictures are uploaded to Plex and locked, so Plex's own refresh keeps them; the one replaced is kept first and comes back on undo. It waits while someone watches on Plex as on Jellyfin. Logos, season posters and title cards stay Jellyfin's (Plex doesn't keep logos).

## [2.29.0] - 2026-10-31

### Added

- **Skip intro and Up next from Plex**: for a film or episode in Plex, the player takes the intro and credits from Plex's own markers (Plex finds them with Plex Pass), as it does from Jellyfin's Intro Skipper.

## [2.28.0] - 2026-10-31

### Added

- **Virtual drive: Usherr adds its libraries to Plex**: with Plex connected, *Add them to Plex for me* on the drive card makes Movies-Stream, TV-Stream and Anime-Stream in Plex, with Plex's own settings for films and shows. A drive folder already in one of your libraries is left as it is.

## [2.27.3] - 2026-10-31

### Fixed

- **Diary: a Plex account linked later brings its whole history**: the Diary reads only what's new since its last look, so an account linked to Plex after that missed what it had watched in Plex before. When where someone watches changes, the Diary now reads their whole history once.

## [2.27.2] - 2026-10-31

### Fixed

- **Sign in with Plex picks an address that keeps working**: for a Plex on the same computer as Docker, Usherr now uses http://host.docker.internal first (after checking, without the token, that it's the same server), instead of a plex.direct address tied to the computer's network address, which can change.
- **A Plex that doesn't answer no longer slows pages down**: after a failed answer Usherr leaves that server alone for five minutes instead of waiting on it each time.

## [2.27.1] - 2026-10-31

### Fixed

- **Plex**: when Usherr can't read who watches on a Plex server, the log says why.

## [2.27.0] - 2026-10-31

### Added

- **Plex households get the personal side too**: the people on a Plex server (its owner, Plex Home members and friends it's shared with) are matched to Usherr accounts by name, like Jellyfin's users, and someone on both servers is one person. The Diary and Wrapped fill in from each account's Plex history; For you and *Because you watched* learn from it; Tonight, Pick for the Room and Catch Me Up choose from what's in Plex's libraries, with Tonight's *Next* episode worked out from the history (and *Continue* from Plex's On Deck for the server's owner). *What we have* and *Complete the collection* count Plex's libraries, a title on the virtual drive as streamed.
- **The Marquee shows what Plex is playing**, with the same details as Jellyfin's: the poster, 4K, Dolby Vision and Atmos, the progress and the device. Read-to-Me also waits while someone watches in Plex.

## [2.26.2] - 2026-10-31

### Fixed

- **The same NZB is fetched from an indexer once**: Prowlarr hands out a new link for the same release at every search, and Usherr remembered NZBs by link, so streaming (Live TV, Stream now, the Stremio and Nuvio add-on, the virtual drive) fetched the same NZB again each time. Indexers such as NZBFinder count those repeats and close accounts that keep doing it. Usherr now remembers each release's NZB by the release itself (indexer, name and size), also when a release is found broken, and parts of Usherr asking for the same release at once share one fetch.

## [2.26.1] - 2026-10-31

### Fixed

- **GitHub checks are green again**: the third-party licence list matches what the release is actually built with, a drive test no longer trips static analysis, and the phone-size header test checks the menu button itself.

## [2.26.0] - 2026-10-31

### Added

- **Virtual drive: streams reach Plex right away**: *Add as stream* now puts the film, or the show's newest season, on the drive at once, one search a film or episode a few seconds apart, and Plex has each a couple of minutes later. A stream's page in Library, Streams has *Put it on the virtual drive now* (the newest season, for a show) for anything already in your streams. Both use the day's searches for the drive, and stop when they run out.
- **Virtual drive: new episodes first**: a newly aired episode of a show you stream goes to the front of the drive's daily search, instead of waiting behind the backlog.

## [2.25.0] - 2026-10-31

### Added

- **Virtual drive: Plex has new titles within minutes**: when a film or episode comes onto the drive, Usherr asks Plex (and any media server with a library on the drive) to look at just that title's folder, instead of waiting for Plex's next scan. It never starts a whole-library scan, which would read the drive end to end. *Where Plex sees the drive* on the card (`U:` to start with) says where it is mounted; a Windows letter there also goes into the mount commands.

### Fixed

- **Plex sign-in**: a server you just added now shows *Added* in the list instead of a second *Add* button.
- **Virtual drive card**: the optional sign-in box lines up with its text.

## [2.24.4] - 2026-10-31

### Added

- **Virtual drive, mount at sign-in (optional)**: the drive card can give you `usherr-drive.cmd` for your Windows Startup folder, which mounts the drive each time you sign in once Usherr answers, with a clear warning that it installs a startup item. A *Where rclone is* box puts rclone's full path into every command, for when rclone isn't on the PATH. The Plex walkthrough asks whether you want it.

### Fixed

- **Virtual drive, Plex advice**: the settings to change are Plex's server-wide ones (Settings → Library and Scheduled Tasks), including untick *Empty trash automatically*, so a moment without the drive doesn't empty its titles; intro and credits detection needs Plex Pass.

## [2.24.3] - 2026-10-31

### Fixed

- **Virtual drive**: Movies, TV and Anime are always on the drive, even before their first film or episode is ready, so Plex can be given all three straight away.

## [2.24.2] - 2026-10-31

### Fixed

- **Virtual drive**: its address (/dav/) answered with Usherr's web page instead of the drive, so rclone couldn't open it.

## [2.24.1] - 2026-10-31

### Fixed

- **Virtual drive**: it no longer spends a search on a film from a later year, which isn't out yet (it already skipped episodes that haven't aired).

## [2.24.0] - 2026-10-31

### Added

- **Virtual drive (for Plex)**: Plex can't play the Streams library's pointer files, so Usherr now also serves streamed films and shows as a read-only drive of ordinary video files (WebDAV), played straight from Usenet, which rclone mounts on the computer that runs Plex (with WinFsp on Windows; both free, installed by you from their own sites). It's on by itself when Plex is the media server. A file is on the drive once its release is known: anything played is kept at once, the rest are looked for a few a day within a daily number of searches, never in a burst. The drive has its own read-only sign-in. Settings → Extras → Virtual drive has the exact rclone commands; Show me how has *Set up Plex with the virtual drive*; the setup wizard and the Start page ask Jellyfin or Plex; *Something not right?* checks the drive.

## [2.23.0] - 2026-10-31

### Added

- **The last Show Me How walkthroughs**: get music by an artist, follow a list from Trakt, IMDb or Letterboxd, choose how files are named, download clients, and connect Jellyfin (58 in all).
- **The Show Me How website** (https://trytotilt.github.io/Usherr/guide/): a page for every walkthrough, made from the walkthroughs themselves, linked from the Start page and the README.
- **Show Me How checks itself**: before every build, each button a walkthrough points at must still exist, or the build stops and says which; and a browser test starts every walkthrough.

### Fixed

- A search on The Pirate Bay that finds nothing no longer shows a fake release called "No results returned" that looked like it could be got. Such placeholder rows are left out for every indexer.
- On a phone, opening the menu puts the keyboard on its first item, not on its close button.
- The tests on GitHub pass again: formatting, two static-check warnings, the job start order with Usherr's own jobs, the list of routes members may use (the restyled posters), the third-party notices, and the browser tests updated for Usherr's menu. The guide no longer says the Streams library needs StreamNZB (Usherr's own streaming engine is enough).

## [2.22.0] - 2026-10-31

### Added

- **How soon the menu slides away** is yours to choose: Settings, General, next to the colours, from right away to 10 seconds (3 to start with), kept in each browser. On a wider screen the open menu now stays while the mouse is over it and slides away that long after the mouse leaves (it used to close after 8 seconds without movement, even with the mouse on it).

## [2.21.0] - 2026-10-31

### Added

- **Show Me How in the setup wizard**: every step of first-run setup has a *Show me how: this step* card, open the first time, saying what the step is, what to do (getting the free TMDB key, where Jellyfin's API key is, what the folder boxes mean) and, for the real choices, a decision card with the good and bad sides and rough costs (start small or switch everything on, free or paid indexers, an unlimited or block Usenet plan, 1080p or 4K) and whether it can be changed later. The last step points to Show me how and *Something not right?* for what comes next.

## [2.20.0] - 2026-10-31

### Added

- **Something not right?** (the Doctor), at the top of the Show me how menu, and offered by the Ask bar when you type that something isn't working: Usherr runs its own checks and shows every part it looked at with a tick or a warning (Usherr itself, film details, Usenet, indexers, VPN, folders and disk space, recent downloads, subtitles, lists), then each problem in plain words with what stops working, a button to fix it and, where there is one, the walkthrough that helps. For administrators; anyone else is pointed to Why didn't my download work and to telling an administrator.

## [2.19.0] - 2026-10-31

### Added

- **Show Me How from the Ask bar**: type a question like "how do I turn a book into an audiobook", "I want to read a comic" or "set up the VPN", and the matching walkthrough is offered right under the bar, one tap away. Plain word matching, no AI, and it stays quiet when you're asking for a film ("a 90s heist movie"). Two walkthroughs got clearer names: *Add a family member, kids too* and *Watch on your phone (Usherr as an app)*.

## [2.18.0] - 2026-10-31

### Added

- **Tips** from Show Me How: now and then (at most once a day, never in the first two minutes, during a walkthrough or while something plays), a small card suggests something worth trying, and only when it's true for you: voting when you haven't lately, a showtime on a weekend with nothing booked, Roulette in the evening, reading comics panel by panel on a phone, completing a film series you have part of (administrators), and your stack when it's waiting. **Show me how** starts the walkthrough; **Not now** waits a week; **Don't remind me** is for good. *Tips now and then* in the Show me how menu turns them all off.

## [2.17.0] - 2026-10-31

### Added

- **Show Me How**, people and setup: let the kids ask instead of add (requests), Usherr in Stremio or Nuvio, get notices on your phone (ntfy); and for administrators, choose what Usherr handles, add a Usenet provider, add an indexer (or bring them from Prowlarr), choose the quality (1080p or 4K, and what each costs in space), keep the TV smooth while downloading (schedules), set up a VPN for torrents, backups and updates, and move over from Radarr, Sonarr or Prowlarr. The first four lead into each other, so a new administrator is walked through the basics in order.

## [2.16.0] - 2026-10-31

### Added

- **Show Me How**, looking after the library: swap a film for a better copy, get subtitles, move a film to another folder, make a Kids shelf (tags as Jellyfin collections), why didn't my download work (Activity, a title's What happened, Wanted), bring in the films and shows you already have, and restyle a poster (Re-Posterr). Walkthroughs about a film you have search your library first, and say so when a film isn't in it, when subtitles are off, or when there's only one library folder.

## [2.15.0] - 2026-10-31

### Added

- **Show Me How**, listening and reading: play some music (and the player: queue, lyrics, its own window), make a playlist, bring in the music you already have, get a book, listen to an audiobook and read an ebook in Usherr Books (how to open it, the player and reader, installing it as an app, e-readers).

## [2.14.0] - 2026-10-31

### Added

- **Show Me How**, movie night and TV: make a Marquee screen (a code to scan on the tablet), steer it from your phone, watch Live TV, make a Live TV channel (channel packs, Build with AI or your own), follow a sports team (it says when Sports isn't set up), and set up Play on TV. The coach card now unfolds a folded settings card, and a page's tab in its address (?tab=) counts as being on it.

## [2.13.0] - 2026-10-31

### Added

- **Show Me How**, finding things: browse Discover (what to show, Filters, Hide what I have, the family filter, recipes), get a TV show (all episodes, only new ones, or ones you pick), follow an actor or director, see what's coming (Calendar, Wanted, Following), fill the gaps in your library (Complete your library), and ask for something in your own words (the Ask bar, What's it like?, Catch me up).

## [2.12.0] - 2026-10-31

### Added

- **Show Me How**, the everyday ones: find something for tonight, let Usherr pick (Roulette), watch a film now or keep it (Stream or Add to library, only offered when streaming is set up), teach Usherr your taste (votes and Rate 20), keep a list of what to watch next (your stack), your diary and your year (Wrapped), add a family member (roles, and requests for children), and Usherr on your phone (a code to scan, then Add to Home Screen; Tailscale for away from home). The coach card opens the side menu by itself when a step is about something in it.

## [2.11.0] - 2026-10-31

### Added

- **Show Me How**: a button in the top bar opens a menu of things you can do, grouped (Watch, Movie night, Listen, Read, My library, Setup) with a search. Each one walks you through it on the real screens, in **Simple** (a few questions, then Usherr presses the buttons for you while you watch; **Let me do it** hands over) or **Advanced** (every option, explained). A coach card lights up each button and never blocks the page; choices come with a decision card (the options, their good and bad sides, what we recommend, and whether you can change it later), and keys are tested as you paste them. A walkthrough can be paused and carried on later. The first ones: book a showtime, set up a pre-show, read a comic (panels or page turn), read a story arc in order, this week's new comics, the comics Review tab, set up comics, turn an ebook into an audiobook, and complete a film series.
- A Start page (https://trytotilt.github.io/Usherr/start/): six steps on Windows from nothing to Usherr and Jellyfin running, each with what you'll see, how to tell it worked and the usual fixes. It writes your compose.yaml from a few answers, in the browser; nothing is sent anywhere.

## [2.10.0] - 2026-10-30

Built on **Mediarium 3.9.0** (everything in Mediarium 3.8.1 to 3.9.0 is in, see [CHANGELOG.md](CHANGELOG.md)): release lists past 60 results, download speed and what is left per client in Activity, extra indexer categories, a VPN test, why a torrent failed, umlaut spellings in searches, and security updates.

### Changed

- A show named in another language (Mediarium 3.9.0's folder language) keeps its existing folder, alongside Usherr's own rule that new episodes go into the show's and the season's existing folders.

## [2.9.7] - 2026-10-30

### Changed

- Pre-show clips: Usherr uses `/data/preshow` by itself when a folder is mapped there (as for trailers), so clips can be dropped in from the computer; a new clips folder starts with the usual categories (Bumpers, Policy, Commercials, Sound, Intermission, Halloween, Christmas), and clips in the old folder are copied over. The Pre-show page says when the folder is out of sight in Usherr's own storage, and how to map one.

## [2.9.6] - 2026-10-30

### Changed

- The top bar's Showtime button is a rounded rectangle in every look (Neu made it an oval).

## [2.9.5] - 2026-10-29

### Added

- Showtime from any page: a Showtime button in the top bar counts down to the next booking (its time, then "Pre-show starts 12:34", "Show starting 07:48", "Now showing"; on a phone the countdown alone) and always opens the Showtime screen. Showtime in the left menu, a line on the Dashboard, Book a showtime on film cards (Library, Streams, Discover), and Book a film on the Showtime page.

## [2.9.4] - 2026-10-29

### Fixed

- Showtime: a film that isn't in the library has its stream found during the pre-show (five minutes before at the latest), trying the next release when one won't start, so it plays at showtime. While it starts, the Showtime page says so ("Starting the film: finding its stream") instead of "No showtime here yet", and if it can't, Try again.
- Showtime plays a film that's in Streams (the streamed library) from its saved release, as its Play button does, instead of looking for a new stream.

## [2.9.3] - 2026-10-29

### Changed

- Change the style, from a title's Re-Posterr, opens the style page trying it on that title (first in Try it on), not a random one.

### Fixed

- A title's Re-Posterr window no longer breaks the page when fanart.tv doesn't answer: fanart.tv and TheTVDB get eight seconds, one that doesn't answer is left out for five minutes, and the window opens with TMDB's pictures.

## [2.9.2] - 2026-10-29

### Added

- Re-Posterr, more of Posterizarr's options: an outline and a shadow on the words, up to 1-4 lines, your own wording for season posters and title cards, collections without "Collection", and titles in other alphabets drawn in a font that has their letters (WenQuanYi Zen Hei for Japanese, Korean and Chinese; DejaVu for the rest). Overlays: your own PNGs laid over the picture, by the copy's resolution too (a 4K badge on the 4K copy). A library's own poster style. Backgrounds with the title on, by hand or in the nightly look. TheTVDB as a source for shows (with a key). Your own picture and your own words on one title. Styles saved as a file and loaded back. The full Posterizarr app connected, able to take over the posters.

## [2.9.1] - 2026-10-29

### Changed

- A poster Re-Posterr puts on a title in Jellyfin (picked or made in your style) shows on Usherr's own pages too: the title page, rows, searches and the rest. Putting the old one back brings TMDB's back in Usherr as well.

## [2.9.0] - 2026-10-29

### Added

- Re-Posterr makes posters in your style (after Posterizarr): art without words with the title in a font and layout of your choosing (Fade, Band, Logo, Centre or Framed to start from; Bebas Neue, Anton, Oswald, Playfair Display or Usherr's own font; place, size, colour, capitals; a fade or band behind; a border), or the title's logo. Posters, season posters ("Season 2") and episode title cards (a still with the episode's name and number). Designed under Your style on the Re-Posterr page, tried live on any title; used on one title (the Re-Posterr button's Your style tab) or by the nightly look (asked first, or by itself), and put back like any change. A new style is offered again. Kept as a versioned recipe of layers, so more of Posterizarr's options can be added.

## [2.8.6] - 2026-10-29

### Changed

- Re-Posterr opens a title quicker: the titles its nightly look keeps say which Jellyfin items it is (Jellyfin's whole list is read only for a title not among them), and fanart.tv's answers are kept half an hour, so choosing a picture doesn't ask again.

## [2.8.5] - 2026-10-29

### Fixed

- Re-Posterr can put a picture on in Jellyfin 12: the upload carries Usherr's key the way Jellyfin wants it (it answered 401 before).

## [2.8.4] - 2026-10-29

### Fixed

- Re-Posterr's fanart.tv pictures show in the side-by-side: Usherr fetches fanart.tv's small copies itself (making one from the picture when fanart.tv has none), and a picture that won't load at all is left out instead of shown as an empty box.
- A title's page on an Android phone no longer shows a stale second copy of its facts (year, runtime, director, writers) while the trailer plays behind them: the header's lines don't keep a layer of their own once they've risen into place.

## [2.8.3] - 2026-10-29

### Fixed

- The Re-Posterr button is on a streamed film's or show's page too (one not in the library), since Jellyfin has it.

## [2.8.2] - 2026-10-29

### Added

- The setup wizard's Extras step (and Settings → Extras) has a Re-Posterr card: what it does, ask first or on its own, turning on its nightly look, the fanart.tv key with Test it, and its page.

## [2.8.1] - 2026-10-29

### Changed

- The button on a film's or show's page that picks its poster, background and logo is called Re-Posterr, like its page.

## [2.8.0] - 2026-10-29

### Added

- Re-Posterr, Usherr's artwork picker (after Posterizarr): it picks the poster, background and logo for each film, show and collection in Jellyfin from TMDB and fanart.tv (your language first, backgrounds without words, big enough, the best liked) and puts them on every copy, at the size a TV tile wants. Ask me first (the default: picks wait beside what Jellyfin has, to use or skip), fill in what's missing, or also replace pictures that are too small. At night, a title every few seconds, never while someone is watching on Jellyfin and not when its metadata disk is low; a booked Showtime's film straight away. Settings → Re-Posterr, a page of its own.
- Change artwork on a film's or show's page: every candidate side by side; a picture chosen by hand goes on at once and is kept. Every picture replaced is kept first, so Put the old one back returns it.

## [2.7.2] - 2026-10-29

### Added

- A guide page for the pre-show (themes, Only when..., schedules, clips, coming-soon trailers, the Jellyfin app), in the in-app guide too, and the guide brought up to date: picking up where you left off, Usherr's look (Neu, main colours, the menu drawer), Read-to-Me and Sports in the README.
- The setup wizard's Extras step has a Pre-show card, and the Setup checklist shows the pre-show in the Jellyfin app, coming-soon trailers, Sports and Read-to-Me.
- Tooltips on the pre-show page's controls: the theme editor, schedules, clips, trailers and the Jellyfin app.

## [2.7.1] - 2026-10-29

### Changed

- The menu goes away with a click or touch anywhere outside it, on a wide screen too.

## [2.7.0] - 2026-10-29

### Added

- Coming-soon trailers downloaded ahead for the pre-show (after NeXroll's NeX-Up): the trailers of films you want, your shows' new seasons and the most talked-about films coming to and showing in cinemas, fetched with yt-dlp within 60 days of the release (4K, or the best each trailer has), every few hours a few at a time, and removed 14 days after the release or once the film is in the library, under a 10 GB cap. They play at once, in the Jellyfin app too; the pre-show takes the household's own and upcoming ones first. Pre-show page → Coming-soon trailers: what's ready and waiting, Watch, Keep, Remove, Look now, and when they come and go.
- New pre-show cards: Coming soon before each trailer (its poster and when it comes), a coming-soon reel of posters, an announcement in your own words, and a QR code for Usherr; drawn live and, for the Jellyfin app, as short videos.
- The image has yt-dlp, its YouTube helper and Deno.

## [2.6.5] - 2026-10-28

### Changed

- The pre-show plays full screen (asked for at the start, kept for the film; a Full screen button when the browser wants a click first, as for a Showtime that starts by itself).
- YouTube trailers play without captions, in the pre-show and on the Marquee (the pre-show also turns off captions a trailer forces on).

## [2.6.4] - 2026-10-28

### Fixed

- A pre-show clip still arriving can't play cut short: uploads are written under a hidden name and named once whole, a file copied into the folder waits until it has settled (20 seconds unchanged), and the Jellyfin pre-show leaves out a clip whose file is still being written.

## [2.6.3] - 2026-10-28

### Added

- The film gets ready while the Jellyfin pre-show plays: Usherr reads its start and end through Jellyfin, so a streamed copy (nzbdav's or a .strm) is fetching before its turn.
- A slot with both sound and picture ticked can play when either matches ("Any ticked sound or picture is enough"), for files whose labels aren't all there.
- The theme editor's One clip: a slot that plays a chosen clip shows (and picks) it.

### Fixed

- The Jellyfin pre-show judges the sound and picture by the copy Jellyfin plays, not the library's own release of the film.

## [2.6.2] - 2026-10-28

### Added

- A pre-show theme's slot can play only in a holiday's season (Only when… → Season: chosen holidays, or any holiday), for clips like a Halloween "Presented by".

## [2.6.1] - 2026-10-28

### Added

- The pre-show in the Jellyfin app: Usherr answers the NeXroll Intros plugin (`/plugin/intros`, with its own key and signed links), so the same pre-show plays before films in every Jellyfin app: your clips, and the cards made into short videos in the Marquee's colours (welcome with the film's poster, the rating, silence your phones, the countdown, the feature presentation). YouTube trailers are left out there for now. The Marquee follows each piece as Jellyfin plays it.
- Pre-show page → In the Jellyfin app: Set it up adds the plugin's repository and installs it, Restart Jellyfin (only when nobody is playing) loads it, and Usherr gives the plugin its address and key. Before films (on), before episodes (off), and the theme.

## [2.6.0] - 2026-10-28

### Added

- The pre-show: Usherr's own, after NeXroll. Clips from a pre-show folder (one folder level is the category: Bumpers, Halloween, Christmas...) or uploaded, themes made of blocks (welcome card, a clip from a category, holiday clips, coming-soon trailers, the rating card, silence your phones, the countdown, the feature), each with "Only when..." conditions (genre, rating, sound and picture formats, movie or episode, time of day), and schedules (holidays built in, dates, weekdays, hours) that pick the theme. "Movie night" and "Quick start" are ready out of the box. Clips and trailers rotate least-lately-played first, and the plan fits a time budget.
- A booked Showtime runs its pre-show so the feature starts on the minute; each person can also have it before any film. Skip and Skip the pre-show are always there. The Marquee follows each piece ("Coming soon" with the poster, the rating, the countdown).
- Settings → Movie night → Pre-show: themes, schedules, clips, theater name and trailer length.

### Fixed

- The settings menu: the group of the page you're on stays open when clicked, the Movie night group (Marquee, Pre-show) is reachable from any page, and on a phone opening a group shows its pages instead of jumping to the first.

## [2.5.6] - 2026-10-28

### Added

- Everything picks up where you left off: movies, episodes, NSFW videos, streams and sports games open at the place you stopped, and comics at the page, kept for each person on the server (`home_positions`), so on any device. The player says "Picked up at …" with a Start over button, and Start over stays in its top bar. Finished (past 95%, or the last 45 seconds) or barely begun (under 15 seconds), it starts at the beginning. Live TV and the channels are live and never resumed.
- Start over in the ebook reader, the audiobook player and the Bookshelf's comic reader; a book or comic read to the end opens at the start again.

### Changed

- Music starts each song from the top: the queue kept from last time no longer picks up part-way through a track.

## [2.5.5] - 2026-10-28

### Added

- A main colour for the whole app, under the highlight colour (Settings → General): Midnight navy, Espresso, Forest, Plum, Oxblood, Ocean, Stone or OLED black, each with a dark and a light look, or the highlight's own tint as before. Kept per browser. Made by `tools/usherr/bases.py` into `usherr-bases.css`.

## [2.5.4] - 2026-10-28

**Changed**
- Posters first: the menu is a hamburger drawer on every screen. On a wider screen it stays open while you move between pages and slides away after 8 seconds with no mouse movement over it (or with the hamburger, or Escape).
- The filter bar of the Library, Discover, Upcoming, Wanted, Diary, Comics and NSFW pulls down from under the top bar; closed, a slim handle says what is showing. It stays open or closed as you left it.
- Rows by sits beside the sort (Music's Genre too), and the Comics kind pick is its own size.

## [2.5.3] - 2026-10-28

**Changed**
- Tonight's picks moved from the wide Dashboard button to the top bar, beside the dice: it opens in a window from any page and asks straight away.

**Fixed**
- Usherr Neu: the news ticker's headlines have room between them instead of running into each other.

## [2.5.2] - 2026-10-28

**Fixed**
- The pinned filter bar (Library, Discover, Upcoming, Wanted, Diary) and the Books header no longer leave a slit under the top bar that posters scroll through.

## [2.5.1] - 2026-10-28

**Changed**
- Usherr Neu: Comics and NSFW cards float in recessed slots and lift on hover, like every other library.

## [2.5.0] - 2026-10-28

**New**
- **Usherr Neu**: a soft, raised look of Usherr's own (light and dark, new icons and font, covers in recessed slots, floating tiles). Pick it in Settings › General › Look; Usherr Classic stays the default.
- **The Read-to-Me page**: making an audiobook moved from the pop-up to a full page, in colour-coded steps that fold, with hints on every part and a guide that walks new readers through it.
- A poster size slider in every library: Music, Ebooks, Audiobooks, Comics, Discover and NSFW (Find too).

**Changed**
- The library totals moved from the Dashboard to Statistics, which now shows every kind (anime, books, comics, sports too) with streams counted; each card opens its library.

## [2.4.0] - 2026-10-27

Built on **Mediarium 3.8.0** (everything in Mediarium 3.4.0 to 3.8.0 is in, see [CHANGELOG.md](CHANGELOG.md)).

**New**
- **Read-to-Me**: make an audiobook from an ebook with Kokoro or Chatterbox. Pick the voice, the speed and the chapters, listen while it is made, and get a notice when it is done. A full cast gives the dialogue its own voices. Presets, a pronunciation list, lines read again, and copied voices (a WAV put in the voices folder becomes a voice).
- **Sports**, stacked on Sportarr with Usherr's own TheSportsDB engine: leagues, teams and games for everyone, games streamed with your language first, live scores and a Dashboard card.
- **Marquee Sports**: scoreboard, tonight's games, score ticker, kick-off countdown, and the game being watched as a matchup card.
- From Mediarium: calendar times in your own time zone, too-short files turned away at import, custom season numbering, a Quality profile filter and sort in the Library, Ended and Continuing filters for shows, more Discover filters (original language, lowest rating, runtime), and much faster big libraries.

**Changed**
- The Library and Settings menus keep Usherr's way: open on their own pages, folded elsewhere, one at a time.

**Fixed**
- Household members can get their own Stremio and Nuvio add-on link: the card is on the **You** page too (Extras is for administrators).
- Each NZB is fetched from the indexer once.

## [2.3.0] - 2026-10-21

Built on **Mediarium 3.3.0**.

**New**
- **Skip intro**: a button in Usherr's player while an intro plays, from the file's chapters (Intro, Opening, OP...) or Jellyfin's Intro Skipper segments; when the credits are known, *Up next* comes up as they start.
- **Releases that play**: Usherr's streaming engine remembers which release groups and indexers open, fail to open, break part-way or get *Try another release*, and puts the ones that keep failing at the back of the list.
- The Marquee shows something that starts playing in Usherr's player within seconds, not at its next look.

## [2.2.0] - 2026-10-21

Built on **Mediarium 3.3.0**.

**New**
- **Showtimes at home**: book a film for a time from its page or your stack, with minutes of pre-show and where it plays (the Showtime page left open on the TV starts it by itself, the TV through Play on TV, or yourself in another app); "right after" another booking makes a double feature. The Marquee shows a countdown band and starts the pre-show on time by itself. The Showtime page lists the bookings, to move or cancel.
- **Up next**: near the end of an episode in Usherr's player, a card offers the next one and plays it after a 10-second countdown, from your file or streamed (only episodes that have aired; not on Live TV). *Play the next episode by itself* is kept on each device.

## [2.1.0] - 2026-10-21

Built on **Mediarium 3.3.0**.

**New**
- **Marquee badges for everything that plays**: the sound (Dolby Atmos, DTS:X, TrueHD, DTS-HD MA, Dolby Digital+... and 7.1, 5.1, Stereo), the picture's shape (2.39:1, 1.85:1, 16:9; measured inside black bars) and the cinema format: how it was shown and shot (IMAX, IMAX 70mm, Dolby Cinema, 70mm, 65mm or 35mm film, Digital, Black & white), from IMDb's technical specifications, with TMDB's release notes when IMDb has nothing; *Cinema formats from IMDb* turns IMDb off. What Usherr plays itself is looked at once, so a file marked HDR10 that is really Dolby Vision says so.
- **Trailers fill** on the Marquee: trailers on a portrait screen can be zoomed to a poster's size or to all the space there is, instead of a small letterboxed strip; the pre-show's trailers and your own videos too.
- **The Marquee shows what's playing, wherever it plays**: Usherr's own player in the browser (library files, Stream now, Live TV with its channel) and its Stremio and Nuvio add-on now count, next to Jellyfin, Emby and StreamNZB, with progress and the picture's badges. *Usherr itself* under *Now showing comes from* turns it off.

**Fixed**
- A Marquee screen keeps only the settings changed for it (a look picked on the remote, say) and follows the shared settings for everything else. Before, picking a look gave it a frozen copy of every setting, so later changes on *Look and scenes* seemed not to work. Screens that had a copy keep just what differed. *Look and scenes* lists what each screen keeps of its own, with a button to drop it.
- Security: built with Go 1.27.3, which fixes a flaw in Go's own certificate checking (GO-2026-6809).
- A long sign such as *Feature presentation* fits a portrait Marquee screen in the Cinema and Posterr looks, and the trailer and title under it stay centred instead of being pushed off to one side.
- A stream played from a title's page shows that title's poster and details on the Marquee; a stream started from a list of releases is looked up by its name.
- The Marquee in Home Assistant could be left showing a screen's old look after a new one was chosen there.

**New** (from Mediarium 3.3.0)
- **SABnzbd or NZBGet for Usenet**: *Settings → Downloading → Usenet → Use SABnzbd or NZBGet* sends Usenet releases to your own downloader instead of the built-in one (off unless switched on; streaming isn't affected).
- **What's inside your files**: codec, resolution, HDR10, HDR10+, Dolby Vision, every audio track (with Atmos) and subtitle track, read from the files' headers in the background and shown on a film's page and each episode. The Library's Filters gain video codec, HDR, resolution, audio and subtitle language and "no {language} audio", each a chip when on. The Marquee's badges use these details for library films.
- **Ask your library from an AI assistant or Home Assistant's voice assistant** (MCP, *Settings → Other apps*; off until switched on, administrator key only, asks before changing anything).
- **Quality order and groups** in a quality profile, like Radarr and Sonarr, with a TRaSH layout.
- **nzb360 and other Radarr/Sonarr phone apps**: release search, history, scores and more.
- Music: rescan an album folder, delete one file, search for missing tracks. Separate size limits for movies and TV, a switch for the built-in minimum size, up to 500 downloads at once, TRaSH folder name presets, settings in the search box, WebP covers.

**Fixed** (from Mediarium 3.3.0)
- Short TV episodes are no longer skipped as too small; a FlareSolverr check no longer times out while it solves; the search list's Score shows the whole score; Open Library searches with odd characters work.
- Everything else in Mediarium 3.2.17 to 3.3.0, listed in [CHANGELOG.md](CHANGELOG.md).

## [2.0.0] - 2026-10-20

Built on **Mediarium 3.2.16**.

**New**
- **A choice of streaming engine**: *Settings → Extras → Streaming engine* picks Usherr's own engine or StreamNZB for Stream now, the streamed library and streamed Live TV. The one chosen goes first; the other is the way out when it finds nothing. StreamNZB stays the default.
- Usherr's engine now serves **Stream now**: movies and episodes are searched on your indexers (daily shows by date), listed best first, and played straight from Usenet. A release that breaks part-way hands over to the next one in line, from where you were; **Try another release** does the same by hand.
- **Device qualities** (4K, 1080p, 720p): the biggest file and the words to leave out, for the TV and for a phone.
- **StreamNZB is optional**: a new install streams with Usherr's own engine and needs nothing else (the setup wizard's Extras step and the setup checklist count either engine as ready). An install that already has StreamNZB connected keeps it until you choose Usherr.
- **Import from StreamNZB**: on the Streaming engine card, copy StreamNZB's Usenet providers, indexers and size limits from its `config.json`. You see what is in it first, what Usherr already has is left alone, only what you tick is copied, and logins are never shown.
- **Real TV picks from the whole lineup**: instead of six network buttons, the dialog lists every channel at your zip code to tick (up to 40 at a time), with search and News, Sports, Movies, Family and Talk filters. *Channels from* chooses the stations on the air (every affiliate and subchannel: MeTV, Cozi, ION, Telemundo...) or a cable or satellite provider's lineup (ESPN, HGTV, TNT...). The main networks start ticked. Channels are built four at a time, and subchannels get their own numbers (4.2 is channel 42).
- **Stremio and Nuvio add-on**: *Settings → Extras → Stremio and Nuvio* gives each person a private link (with a QR code and an Install in Stremio button on an https address) to add once in each app. The apps then show *Usherr Movies*, *Usherr TV*, *Usherr Watchlist* (thumbs up) and *Usherr Live TV*, and any title they open gets streams: your library's file first, then the best releases Usherr's engine finds on your indexers, opened when picked and handing over to the next if one can't be read. Subtitles come from your library's files. *Make a new link* stops the old one.
- **Live TV tunes in without a long wait**: when a streamed program's release takes more than a few seconds to find, the channel stays on its static and asks again shortly, instead of holding the page; every asker shares one lookup. A few minutes after start-up (and every three hours) the programs on now, or starting within half an hour, on every channel are looked up in advance, three at a time.
- **Fewer failed tunes**: a stream that can't be read at first look is looked at once more before the channel falls back to its backup, and the log now says why a look failed (ffprobe's own words).
- **Real TV's AI has room for every slot**: the answer's length now grows with the number of slots (a full day with backups ran past 4000 tokens and was asked twice), and a cut-off answer is asked again with up to 16000.
- A **Right now** panel on the card: speed, connections in use on each Usenet server, what is streaming and the searches each indexer has done today.
- **Sound in the browser, and 4K stays 4K**: a stream in Dolby (E-AC3, Atmos), TrueHD or DTS no longer plays silently in Chrome and Edge. The picture plays as it is and the sound comes beside it, converted to AAC with its channels, in step and seeking as usual. Every conversion now keeps what the browser plays: a picture it shows is copied (4K, HDR, HEVC), a sound it plays is copied, 5.1 stays 5.1, and nothing is made smaller unless you pick a lower quality. That holds for files on disk (converted by Jellyfin or by Usherr), streams and Live TV, which no longer turns pictures into 720p.
- **Sound sync**: the sound played beside a stream's picture allows for the delay your speakers add (Dolby Atmos or DTS:X processing, a receiver, Bluetooth), and the player's *− Sync +* buttons move it earlier or later, 50 ms a tap, kept on each device. Releases whose sound begins a moment after the picture start in step.
- **Marquee screens in your own order**: up and down arrows on *Marquee → Screens* (and the phone remote) set the order the screens are listed in; it stays put instead of moving whichever is on to the top.
- **Pick the language of a stream**: *Audio & subtitles* in the player now lists a stream's sound tracks too, and a MULTi release starts in the language you listen in, even when the file's first track is another.
- **Your stack**: the films and shows you want to watch next, as a stack of tickets in the menu. Put a title on it with the stack button on any poster card (Discover, the Library) or on its page; swipe through it (or use the arrows), move a title up or down, take it off, and tap the poster for its page or **Play now** (a film that isn't downloaded offers Stream). Each person has their own, kept on the server, so a phone has it too. To make room, *Settings* folds away in the menu until you open it.
- **The Marquee gets its own page** (*Marquee* in the menu, and in Settings), with tabs for Screens, Look and scenes, Slides and pre-show, and Home Assistant and API:
  - Every screen is listed, with what it shows now. A screen can have settings of its own, and a **phone remote** (`/marquee/remote`) steers it: previous, pause, next, pin, dim, blank, info, the pre-show, its look and *Show now*.
  - **Seven looks** (Usherr, Cinema, One-sheet, the Poster wall and more), each with its own themes, fonts, frames and effects.
  - New scenes: tonight on Live TV, spotlights, trivia, your own **slides** (pictures, videos, web pages and words), a **pre-show** before the film, and **theme music**.
  - **Home Assistant** (over MQTT: each screen appears by itself, with buttons, switches for pause, pin, dim and blank, and its look and scene) and an **API** for scripts and buttons.
- **Ticket services for In theaters**: *Tickets from* chooses Fandango (the default in the US), Atom Tickets, Cinemark, Moviefone, Showtimes.com, a link of your own (with `{title}` and `{zip}`), or none. Each service's button can show a picture you upload, such as its logo. With Fandango, *All showtimes near* lists everything playing near your ZIP.
- The setup wizard's Extras step has the **In theaters** and **Marquee** cards, the setup checklist counts them, and the first-visit tour points at your stack.
- **The user guide** has pages of its own for Live TV, the AI and the Marquee, and the gaps elsewhere are filled, with new pictures.

**New** (from Mediarium 3.2.16)
- **Custom formats from the TRaSH Guides** are scored with TRaSH's own scores, can be added to several profiles at once, and can be kept up to date by themselves (*Keep TRaSH formats up to date*, daily or weekly) or with *Update from TRaSH*, which shows what would change first.
- **Why a release was picked**: a manual search marks the release an automatic search would take as *Best match*, and an arrow after each score lists the custom formats behind it.
- **Minimum seeders** on each torrent indexer, and stopping or removing a download also removes it from qBittorrent or Transmission (a switch on the download client card).
- **Country for release dates** under *Settings → Info, lists and subtitles*; a film's page shows when it's in cinemas and when it comes out digitally, for your country.
- Indexers are listed by priority in compact cards, and the problem report form says when a newer version is out.

**Fixed** (from Mediarium 3.2.16)
- An indexer's seeding rules now reach the download client; SubDL subtitles download again; shows with the same name and folders shared by several shows are matched correctly.
- Everything else in Mediarium 3.1.4 to 3.2.16, listed in [CHANGELOG.md](CHANGELOG.md).

## [1.0.4] - 2026-10-17

Built on **Mediarium 3.1.3**.

**New** (from Mediarium 3.1.3)
- **Signed-in devices**: *Settings → Account & Access → Security* lists them, with *Sign out all other devices*.
- **Keep the original file names**: a switch under *Settings → Library → File names and importing*; custom naming gets `{Original Filename}` and `{Original Title}`.
- Account changes and API keys made or removed are written to the log and Activity for administrators.
- Change a show's folder, locate a renamed file, set your own book cover, and administrators can see the folder of a title's files.

**Fixed** (from Mediarium 3.1.3)
- Titles with accents or in other alphabets (Pokémon, Amélie, Japanese, Russian, Greek) are found, matched and imported.
- Password-protected RAR releases open with their password; a release whose link gives a login page is no longer grabbed again and again.
- Big libraries are much faster: the Dashboard, Wanted (now in pages) and the Library tabs.
- Security: the setup code can't be skipped, sign-in limits hold however fast the guesses come, and a site entry can't be pointed at your home network.
- A learned daily indexer limit is forgotten after a week and can't lock an indexer out for good.
- Torrents already running when a VPN connects, or the kill switch is switched on, are stopped within about a minute instead of sending from your home address.
- Everything else in Mediarium 3.1.3, listed in [CHANGELOG.md](CHANGELOG.md).

## [1.0.3] - 2026-10-16

Built on **Mediarium 3.1.2**.

**New**
- **Daily limits for indexers** (from Mediarium 3.1.2): Usherr learns each indexer's daily request limit, stops automatic searches just before it, shows "12 of 25 requests today" on the indexer's card and starts again after midnight (UTC). An indexer can also be given a known limit under *Requests per day*.
- The menu lists every Library tab under *Library*, Anime, Comics and NSFW 18+ included; they fold up under Library, like a settings group.
- **Calendar search**: the search and AI bar now sits at the top of the calendar too. Search narrows the calendar as you type and lists the matches by date (a date opens that month); Ask takes questions like "anime this weekend" or "superhero films next month".
- The calendar's kinds are a drop-down next to its tabs, with *In theaters* as a kind of its own.
- Streamed shows are on the calendar too: new episodes of shows you stream (but don't keep in the library) appear marked *Streams*, looked up on TMDB about twice a day.
- **A shorter Dashboard**: the search and AI bar moved into the top bar, where it works from every page; Roulette is the dice next to it; *What should I watch tonight?* is a button of its own; and the folded calendar is gone (*Coming up* shows the same).
- **News in one line**: the Dashboard's news is a single line at the top, one headline at a time or a ticker, and *All news* opens the full list. Each person picks the style, or turns it off, under *Settings → General* or in All news.
- The Dashboard has **Anime** and **Comics** cards next to the others, each with its own notch; the TV card now counts TV without the anime.
- **A slim toolbar on Discover and the Library**: one row with the kind (a drop-down), Filters (it says how many are on) and the sort, pinned under the top bar as you scroll. Filters that are on show as small chips with an x. The Library's status (Downloaded, Waiting…) is one of its filters. The pages' own search and AI bars are gone: the top bar's does the same and now knows which page and kind you're on. *Complete your library* comes after the first row instead of before the posters.
- **Live TV programmed like a network**: a channel can start its shows on the hour and every 15, 30 or 60 minutes, give each part of the day (Morning, Mid-morning, Afternoon, Evening, Prime time, Late night, Overnight) a line-up of its own, and pin shows or movies to set times ("Seinfeld, weeknights at 7", a Sunday Night Movie). The time between shows plays bumpers and ads from a folder you choose, then an "Up next" card, drawn in the browser and in Jellyfin.
- **Live TV guide looks and surfing**: the guide can be a modern grid, a '90s cable box, the Prevue Channel (a preview on top, the line-up rolling below), newspaper listings or "Now on" tiles; each column can cover 15, 30 or 60 minutes, 6 to 24 hours ahead. Star favourite channels, hide others. While watching: channel up and down (arrow keys, buttons or a swipe), type a number, *Last* channel, a burst of static between channels (with its hiss, if you like), an info banner with what's on, the time left and what's next, and an optional CRT look.
- **More Live TV**:
  - Special programming: holiday episodes in their season (Halloween to the Fourth of July), new episodes premiering in prime time marked NEW, family hours (nothing for older than about ten before 9 PM), and commercial breaks inside shows from the filler folder.
  - Channel packs (Saturday Morning, Sitcom Gold, TGIF, Sci-Fi, Crime & Mystery, Movie Channel, Creature Feature, Holiday, Rewind, Discovery Hour) and *Build with AI*: describe a channel and it's drafted from your library, with famous shows you don't have streamed.
  - Remind me (a phone message five minutes before a show starts; *TV reminders* in Notifications), Start over, catch-up on the last three hours, and Multiview (four channels at once).
  - *Other apps*: the channel list and guide for IPTV apps such as TiviMate, and a tuner address for Plex's Live TV & DVR.
  - The Marquee has an *On Live TV* scene.
- **Tidier screens everywhere**:
  - Settings pages fold each card to its title row (with its status); tap one to open it, or *Open all*. A card with an error opens by itself. Extras goes from about thirteen phone screens to two.
  - Music, Ebooks, Audiobooks, Wanted and Discover's *Browse all* use the same slim toolbar; Comics and NSFW 18+ pick the kind from the same drop-down.
  - Activity's counts are one line, and *Import a file* and *Add a download* by hand are in one *By hand* menu.
  - A film's or show's trailers are one *Trailers* button, and Reviews, Website, IMDb and TMDB one *Links* menu.
  - On the Calendar the top bar searches the calendar; *Only what isn't downloaded* and *Add to your calendar app* are in its ⋯ menu.
  - The Diary's kind and year are drop-downs in one row.
- Titles you stream but haven't downloaded say **Streams** in search, on Discover and on their page, and adding one says you already stream it before it downloads a copy.
- A streamed title in the Library opens its own page, like any other title, instead of the separate Streamed list. A streamed show's page has *Watch (streams)*, which opens its episodes right there.

**Changed**
- The NSFW 18+ module is called **NSFW** everywhere: its page is at `/nsfw` (old `/scenes` links still work), its folder variable is `NSFW_DIR` (`SCENES_DIR` is still read), and an item in it is a **video**. Existing libraries move over on their own when you update; nothing is lost.

**Fixed**
- **Folders for comics, anime and NSFW 18+ on a new install.** They are set under *Settings → Library → Folders*, next to movies and TV. With nothing set, comics and NSFW 18+ go in `/data/comics` and `/data/nsfw` (made on the first download), and anime finds `/data/anime` when it exists. The example compose file and the install guide list `COMICS_DIR`, `ANIME_DIR` and `NSFW_DIR`.
- Mediarium's own connection checks no longer use up free indexers' daily requests (from Mediarium 3.1.2).
- Everything else in Mediarium 3.1.1 and 3.1.2, listed in [CHANGELOG.md](CHANGELOG.md).

## [1.0.2] - 2026-10-16

Built on **Mediarium 3.1.0**.

**New**
- **In theaters**: an "In theaters now" row on the Dashboard and at the top of Library → Movies, cinema openings on the calendar, and on a film's page links to its showtimes at your theater and everywhere else it's playing. Set your ZIP and theater under *Settings → Extras → In theaters*.
- **Marquee**: a lobby display for a tablet next to the TV, at `/marquee`. It shows what's playing now (from Jellyfin or Emby, and from StreamNZB for Nuvio or Stremio) with a progress bar, featured posters with their trailers, what just arrived, what's in theaters and the week ahead, in large print. Tall scenes scroll slowly. *Settings → Extras → Marquee* sets the order and length of each scene, the sign's own words, the top bar, sizes, trailer sound, quiet hours and colours.
- **Anime folder**: *Settings → Library → Folders* has an anime folder next to the TV folder (or set `ANIME_DIR`). New anime shows go there, and the Library's Anime tab finds them by it.
- Entertainment news starts with three headlines; the heading opens the rest.

**Fixed**
- The Comics settings tab no longer crashes on a new install.
- Comics and films cut into numbered pieces (`.cbr.000`, `.mkv.001`...) are joined back together after downloading.
- Comic releases in another language (German, French, Italian, Spanish, Dutch, Portuguese) are turned down.
- The calendar labels comics as comics and anime as anime, not as movies.

## [1.0.1] - 2026-10-15

Built on **Mediarium 3.1.0**.

- **Colours**: pick Usherr's colours under *Settings → General*: Wine, Cherry, Amber, Emerald, Teal, Blue, Violet, Rose or Graphite, in dark or light. The icon and the browser tab's icon take the colour too. Each person picks their own; it's kept in their browser.
- Skipping a release that's already downloaded no longer leaves the title showing "downloading" for a moment.
- The AI's answers (Ask, why a download failed) call the app Usherr.
- The logo no longer drifts among the shapes behind pages.

## [1.0.0] - 2026-10-15

The first release of Usherr, built on **Mediarium 3.1.0**.

- **For you**: a taste for each person from thumbs up and down and what they watch, with For you rows, a match on every title and why; **Rate 20** to get started.
- **You**: your taste, your votes, and taste in common with the household when you share it.
- **Diary** filled in from Jellyfin, with stars and notes, and imports from Letterboxd and Trakt; **Wrapped**, your year in watching.
- **Tonight**, **Pick for the Room** and **Roulette**; **Discover recipes**, ready-made rows, your services, a family filter and following people.
- **Streaming** from Usenet through StreamNZB, a streamed library next to your files, and **Streams for you**.
- **Live TV** channels from your library; **Comics** from ComicVine with a page-turning reader.
- A news feed, critic scores, complete your library, schedules and quiet hours, a guided setup and an in-app guide.
- **NSFW 18+** stays locked unless the server allows it (`USHERR_ALLOW_ADULT=1`), then needs an adults-only confirmation.
- Usherr's own name, logo and cinema look, its own updates and signing key, and nothing sent to anyone: no usage numbers, no private reports.
