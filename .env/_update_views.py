#!/usr/bin/env python3
"""One-shot helper to update GITHUB_DEST_BOARD_ADDITIONAL_VIEWS in sig-auth-search.azure.env."""
import re

f = '.env/sig-auth-search.azure.env'
content = open(f).read()

# Match this exact block (comment + export line including the comma-separated value)
old_pattern = r'# View names to auto-create[^\n]*\n# Each view will show[^\n]*\n# Existing views are preserved[^\n]*\nexport GITHUB_DEST_BOARD_ADDITIONAL_VIEWS="[^"]*"'

new_text = """# Views to auto-create on the destination board (one per line).
# Format: ViewName=Field1,Field2,Field3
#   - "=" separates view name from visible field names (comma-separated)
#   - No "=" creates the view with default columns
# Uses the REST API for reliable view creation.
export GITHUB_DEST_BOARD_ADDITIONAL_VIEWS="
KEPs=Tracking Status,Stage,PRR Status,Source,Stream
Active Epics=Epic,Stream,Priority,Bet
Epic Pivot=Epic,Stream,Source
Repo Pivot=Source,Stream,Priority
Current Issues=Source,Stream,Priority
No Dependencies
All Issues
Done Last Month
\""""

new_content = re.sub(old_pattern, new_text, content, flags=re.DOTALL)
if new_content == content:
    print('WARNING: regex did not match. Looking for the line...')
    for i, line in enumerate(content.split('\n'), 1):
        if 'GITHUB_DEST_BOARD_ADDITIONAL_VIEWS' in line:
            print(f'  Line {i}: {line[:120]}')
else:
    open(f, 'w').write(new_content)
    print('Updated sig-auth-search.azure.env successfully')
