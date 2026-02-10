#!/usr/bin/env python3
"""Analyze the cached JSON to understand item distribution."""
import json, sys, glob, os
from collections import Counter

script_dir = os.path.dirname(os.path.abspath(__file__))
files = sorted(glob.glob(os.path.join(script_dir, ".cache", "*.json")))
if not files:
    print("No cache files found"); sys.exit(1)

path = files[-1]
print(f"Reading: {path}")
with open(path) as f:
    data = json.load(f)

print(f"Total items: {len(data)}\n")

# Types
types = Counter(i.get("type","?") for i in data)
print("=== Item Types ===")
for t, c in types.most_common(): print(f"  {t}: {c}")

# States
states = Counter(i.get("state","") for i in data)
print("\n=== States (Issue/PR state) ===")
for s, c in states.most_common(): print(f"  {repr(s)}: {c}")

# Status field (from fields map)
statuses = Counter(i.get("fields",{}).get("Status","(none)") for i in data)
print("\n=== Status Field (Project Board) ===")
for s, c in statuses.most_common(): print(f"  {s}: {c}")

# Stage field
stages = Counter(i.get("fields",{}).get("Stage","(none)") for i in data)
print("\n=== Stage Field ===")
for s, c in stages.most_common(): print(f"  {s}: {c}")

# PRR field
prrs = Counter(i.get("fields",{}).get("PRR","(none)") for i in data)
print("\n=== PRR Field ===")
for p, c in prrs.most_common(): print(f"  {p}: {c}")

# Labels (top 30)
labels = Counter()
for i in data:
    for l in i.get("labels",[]):
        labels[l] += 1
print("\n=== Top 30 Labels ===")
for l, c in labels.most_common(30): print(f"  {l}: {c}")

# SIG labels
sigs = Counter()
for i in data:
    for l in i.get("labels",[]):
        if l.startswith("sig/"):
            sigs[l] += 1
print("\n=== SIG Labels ===")
for s, c in sigs.most_common(): print(f"  {s}: {c}")

# Repos
repos = Counter(i.get("repo","?") for i in data)
print("\n=== Repos (top 30) ===")
for r, c in repos.most_common(30): print(f"  {r}: {c}")

# Source project titles
projects = Counter(i.get("project_title","?") for i in data)
print("\n=== Source Projects (top 30) ===")
for p, c in projects.most_common(30): print(f"  {p}: {c}")

# Cross-tab: State x Status
print("\n=== State x Status Cross-tab ===")
cross = Counter()
for i in data:
    st = i.get("state","(empty)")
    status = i.get("fields",{}).get("Status","(none)")
    cross[(st, status)] += 1
for (st, status), c in cross.most_common():
    print(f"  State={st:10s}  Status={status:20s}  count={c}")

# Lifecycle labels
lifecycle = Counter()
for i in data:
    for l in i.get("labels",[]):
        if l.startswith("lifecycle/"):
            lifecycle[l] += 1
print("\n=== Lifecycle Labels ===")
for l, c in lifecycle.most_common(): print(f"  {l}: {c}")

# Priority labels
priority = Counter()
for i in data:
    for l in i.get("labels",[]):
        if l.startswith("priority/"):
            priority[l] += 1
print("\n=== Priority Labels ===")
for p, c in priority.most_common(): print(f"  {p}: {c}")

# How many items have author set
has_author = sum(1 for i in data if i.get("author"))
print(f"\n=== Items with author: {has_author} / {len(data)} ===")

# How many have assignees
has_assignees = sum(1 for i in data if i.get("assignees"))
print(f"=== Items with assignees: {has_assignees} / {len(data)} ===")

# Simulate progressive filtering
closed_merged = sum(1 for i in data if i.get("state","") in ("CLOSED","MERGED"))
open_items = [i for i in data if i.get("state","") not in ("CLOSED","MERGED")]
done_items = [i for i in open_items if i.get("fields",{}).get("Status","") == "Done"]
open_not_done = [i for i in open_items if i.get("fields",{}).get("Status","") != "Done"]

rotten = [i for i in open_not_done if "lifecycle/rotten" in i.get("labels",[])]
stale = [i for i in open_not_done if "lifecycle/stale" in i.get("labels",[])]
frozen = [i for i in open_not_done if "lifecycle/frozen" in i.get("labels",[])]

after_rotten = [i for i in open_not_done if "lifecycle/rotten" not in i.get("labels",[])]
after_stale = [i for i in after_rotten if "lifecycle/stale" not in i.get("labels",[])]

print(f"\n=== Progressive Filter Simulation ===")
print(f"  Total:                           {len(data)}")
print(f"  - Closed/Merged:                 {closed_merged}")
print(f"  = Open:                          {len(open_items)}")
print(f"  - Status=Done (but still open):  {len(done_items)}")
print(f"  = Open + not Done:               {len(open_not_done)}")
print(f"  - lifecycle/rotten:              {len(rotten)}")
print(f"  = After removing rotten:         {len(after_rotten)}")
print(f"  - lifecycle/stale:               {len(stale)}")
print(f"  = After removing rotten+stale:   {len(after_stale)}")
print(f"  (lifecycle/frozen in remainder:  {sum(1 for i in after_stale if 'lifecycle/frozen' in i.get('labels',[]))})")

# All custom field names
all_fields = Counter()
for i in data:
    for k in i.get("fields",{}):
        all_fields[k] += 1
print("\n=== All Custom Field Names ===")
for f, c in all_fields.most_common(): print(f"  {f}: {c}")
