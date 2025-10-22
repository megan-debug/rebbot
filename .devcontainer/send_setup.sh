#!/usr/bin/env bash
set -euo pipefail
cat > send.sh <<'EOS'
#!/usr/bin/env bash
FROM="admin@example.com"
SUBJECT="Special Offer!"
BODY_FILE="offer.html"
LIST_FILE="all.txt"

if [ ! -f "$BODY_FILE" ]; then echo "Missing $BODY_FILE" >&2; exit 1; fi
if [ ! -f "$LIST_FILE" ]; then echo "Missing $LIST_FILE" >&2; exit 1; fi

while IFS= read -r email || [ -n "$email" ]; do
  /usr/sbin/sendmail -t <<MSG
To: $email
From: $FROM
Subject: $SUBJECT
MIME-Version: 1.0
Content-Type: text/html

$(cat "$BODY_FILE")
MSG
  sleep 0.5
done < "$LIST_FILE"
EOS
chmod +x send.sh

# placeholders
if [ ! -f "offer.html" ]; then
  cat > offer.html <<HTML
<html><body><h1>Special Offer</h1><p>Replace this body with your HTML offer.</p></body></html>
HTML
fi
if [ ! -f "all.txt" ]; then
  cat > all.txt <<TXT
recipient@example.com
TXT
fi
echo "send.sh and placeholders created. Run ./send.sh when ready."
