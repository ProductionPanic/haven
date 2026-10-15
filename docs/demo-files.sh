#!/bin/sh
# Creates the local side of docs/files.tape in the current directory.
set -e
mkdir -p theme/assets
printf '# Release notes\n\n- New checkout flow\n- Faster product images\n' > release-notes.md
printf 'body {\n  margin: 0;\n  font-family: system-ui;\n}\n' > theme/style.css
printf '<?php\nget_header();\nthe_content();\nget_footer();\n' > theme/index.php
printf 'User-agent: *\nDisallow: /wp-admin/\n' > robots.txt
