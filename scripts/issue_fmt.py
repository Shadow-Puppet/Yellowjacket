#!/usr/bin/env python3
"""JSON encoding and formatting for scripts/issue.sh.

It is a separate file rather than a heredoc for one reason: an issue body is
arbitrary prose, and every attempt to build that JSON inside the shell ends in
nested quoting nobody can read or verify.  Here the shell passes only argv and
stdin, and every string that reaches the API is encoded by json.dumps.

A Gitea error is a JSON object with "message", and it arrives with HTTP 200 in
enough cases that printing it as data is how a wrong token scope reads as an
empty tracker.  check() is what turns it into a non-zero exit instead.
"""

import json
import sys
import urllib.parse


def die(msg):
    sys.exit("issue.sh: " + msg)


def load():
    raw = sys.stdin.read()
    if not raw.strip():
        die("empty response from the API")
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        die("unreadable response: " + raw[:200])


def check(d):
    if isinstance(d, dict) and "message" in d and "number" not in d:
        die(d["message"])
    return d


def emit(d):
    json.dump(d, sys.stdout)


def names(items, key="name"):
    return ", ".join(i[key] for i in items) or "-"


def main(argv):
    cmd = argv[1] if len(argv) > 1 else ""
    args = argv[2:]

    if cmd == "check":
        emit(check(load()))

    elif cmd == "urlquote":
        print(urllib.parse.quote(args[0]))

    elif cmd == "login":
        print(check(load())["login"])

    elif cmd == "list":
        for i in check(load()):
            labels = ",".join(x["name"] for x in i["labels"])
            who = ",".join(a["login"] for a in (i.get("assignees") or [])) or "-"
            title = i["title"][:62]
            print("#%-4d %-6s %-8s %-62s [%s]" % (i["number"], i["state"], who, title, labels))

    elif cmd == "show":
        d = check(load())
        print("#%d  %s" % (d["number"], d["title"]))
        print("state:     " + d["state"])
        print("labels:    " + names(d["labels"]))
        print("assignees: " + names(d.get("assignees") or [], "login"))
        print("url:       " + d["html_url"])
        print()
        print(d.get("body") or "(no body)")
        print()

    elif cmd == "deps":
        d = check(load())
        if not d:
            print("  (none)")
        for i in d:
            print("  #%d [%s] %s" % (i["number"], i["state"], i["title"][:70]))

    elif cmd == "comments":
        d = check(load())
        if not d:
            print("  (none)")
        for c in d:
            print("  %s (%s): %s" % (c["user"]["login"], c["created_at"][:10], c["body"][:600]))

    elif cmd == "labels":
        for label in check(load()):
            print("%-24s %s" % (label["name"], (label.get("description") or "")[:60]))

    elif cmd == "label-id":
        have = {label["name"]: label["id"] for label in check(load())}
        if args[0] not in have:
            die("no such label: " + args[0])
        print(have[args[0]])

    elif cmd == "label-ids":
        want = [s.strip() for s in args[0].split(",") if s.strip()]
        have = {label["name"]: label["id"] for label in check(load())}
        missing = [w for w in want if w not in have]
        if missing:
            die("no such label(s): " + ", ".join(missing))
        emit([have[w] for w in want])

    elif cmd == "wrap-body":
        body = sys.stdin.read().strip()
        if not body:
            die("refusing to post an empty comment")
        emit({"body": body})

    elif cmd == "new-issue":
        title, ids = args[0], json.loads(args[1])
        body = sys.stdin.read().strip()
        if not body:
            die("an issue needs a body — the tracker is the record, not the title")
        emit({"title": title, "body": body, "labels": ids})

    elif cmd == "created":
        d = check(load())
        print("created #%d  %s" % (d["number"], d["html_url"]))

    elif cmd == "assignees":
        print(",".join(a["login"] for a in (check(load()).get("assignees") or [])))

    elif cmd == "assign":
        emit({"assignees": list(args)})

    elif cmd == "add-label-ids":
        emit({"labels": json.loads(args[0])})

    elif cmd == "issue-meta":
        owner, _, name = args[0].partition("/")
        emit({"owner": owner, "repo": name, "index": int(args[1])})

    else:
        die("unknown formatter command: " + cmd)


if __name__ == "__main__":
    main(sys.argv)
