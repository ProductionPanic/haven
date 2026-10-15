#!/bin/sh
# Seeds a throwaway database with fictional hosts for docs/demo.tape:
#   export HAVEN_DB=$(mktemp -d)/demo.db && sh docs/demo-hosts.sh && vhs docs/demo.tape
set -e
haven add acme-shop deploy@web1.acme.example --path /var/www/shop -t wordpress,woocommerce -e production --notes "Main webshop. Deploys via CI only."
haven add acme-staging deploy@web1.acme.example --path /var/www/staging -t wordpress -e staging
haven add blog www@blog.example.org --path /srv/blog -t hugo
haven add db-primary admin@10.0.4.12 -J bastion -t postgres -e production --notes "Primary Postgres. Backups at 03:00."
haven add bastion ops@ops.example.net -p 2222 -t infra
haven add grafana grafana@ops.example.net -t infra,monitoring
haven add homelab me@nas.local --path /volume1 -t personal
haven add k3s-node-1 root@k3s-1.lab.example -t k8s
haven add k3s-node-2 root@k3s-2.lab.example -t k8s
haven add mailserver mail@mx1.example.org -t mail -e production
haven add test-site test@productionpanic.com --notes "Test site for the file manager demo."
