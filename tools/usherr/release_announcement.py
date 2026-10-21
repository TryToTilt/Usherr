#!/usr/bin/env python3
"""Usherr: a release's Discord announcement, from its section of
USHERR-CHANGELOG.md, as the JSON a Discord webhook takes.

    release_announcement.py USHERR-CHANGELOG.md 2.1.0 owner/repo v2.1.0

The announcement is one embed: "Usherr 2.1.0", what it is built on, links to
the release and the Docker image, and a field per heading of the section
("New", "New (from Mediarium 3.3.0)", "Fixed"...), one short line per item.
It keeps to Discord's limits (1024 characters a field, 6000 an embed).
"""
import json
import re
import sys

FIELD_MAX = 1024
EMBED_MAX = 5800  # Discord allows 6000; room for the title and footer
ITEM_MAX = 180


def section(text, version):
    """The lines of the changelog section for version."""
    lines = text.splitlines()
    head = re.compile(r"^## \[" + re.escape(version) + r"\]")
    out, inside = [], False
    for line in lines:
        if head.match(line):
            inside = True
            continue
        if inside and line.startswith("## ["):
            break
        if inside:
            out.append(line)
    return out


def short(item):
    """One item, kept short: its first sentence, or cut at a word."""
    item = re.sub(r"\s+", " ", item).strip()
    m = re.match(r"(.+?[^.][.!?])(\s|$)", item)  # a sentence's end, not an ellipsis
    if m and len(m.group(1)) >= 40:
        item = m.group(1)
    if len(item) > ITEM_MAX:
        item = item[: ITEM_MAX - 1].rsplit(" ", 1)[0]
        # bold or italic cut in two is closed again
        if item.count("**") % 2:
            item += "**"
        if item.replace("**", "").count("*") % 2:
            item += "*"
        item += "…"
    return item


def absolute_links(item, repo, ref):
    """Links to files in the repository work from Discord too."""
    return re.sub(
        r"\]\((?!https?://)([^)]+)\)",
        lambda m: "](https://github.com/%s/blob/%s/%s)" % (repo, ref, m.group(1)),
        item,
    )


def groups(lines):
    """The section's headings ("**New**") and their items."""
    built, out, name, item = "", [], None, None

    def flush():
        nonlocal item
        if item is not None and name is not None:
            out[-1][1].append(item)
        item = None

    for line in lines:
        m = re.match(r"^Built on \*\*(.+?)\*\*", line)
        if m:
            built = m.group(1)
            continue
        m = re.match(r"^\*\*(.+?)\*\*\s*(.*)$", line)
        if m and not line.startswith("- "):
            flush()
            name = (m.group(1) + (" " + m.group(2) if m.group(2) else "")).strip()
            out.append((name, []))
            continue
        if line.startswith("- "):
            flush()
            item = line[2:]
        elif line.startswith("  ") and item is not None:
            if line.strip().startswith("- "):
                continue  # a sub-item: the item's first line says enough
            item += " " + line.strip()
        elif not line.strip():
            flush()
    flush()
    return built, [(n, items) for n, items in out if items]


def announcement(changelog, version, repo, tag):
    lines = section(changelog, version)
    if not lines:
        raise SystemExit("No section for %s in the changelog." % version)
    built, heads = groups(lines)
    url = "https://github.com/%s/releases/tag/%s" % (repo, tag)
    image = "ghcr.io/%s:%s" % (repo.lower(), version)
    desc = []
    if built:
        desc.append("Built on **%s**." % built)
    desc.append("[Release notes](%s) · Docker: `%s`" % (url, image))
    fields, total = [], len(desc[0]) + len(desc[-1]) + 60
    for name, items in heads:
        value, left = "", len(items)
        for it in items:
            line = "• " + absolute_links(short(it), repo, tag) + "\n"
            more = "…and %d more" % (left)
            if len(value) + len(line) + len(more) > FIELD_MAX or total + len(value) + len(line) + len(name) > EMBED_MAX:
                value += more
                break
            value += line
            left -= 1
        value = value.strip()
        if not value:
            continue
        fields.append({"name": name[:256], "value": value})
        total += len(name) + len(value)
        if len(fields) == 25 or total >= EMBED_MAX:
            break
    embed = {
        "title": "Usherr %s" % version,
        "url": url,
        "description": "\n".join(desc),
        "color": 0x8B1E3F,
        "fields": fields,
        "footer": {"text": "github.com/%s" % repo},
    }
    return {"username": "Usherr", "embeds": [embed]}


def main():
    if len(sys.argv) != 5:
        raise SystemExit(__doc__)
    path, version, repo, tag = sys.argv[1:]
    with open(path, encoding="utf-8") as f:
        text = f.read()
    sys.stdout.reconfigure(encoding="utf-8")
    print(json.dumps(announcement(text, version, repo, tag), ensure_ascii=False))


if __name__ == "__main__":
    main()
