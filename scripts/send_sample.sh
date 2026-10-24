#!/bin/sh
set -eu

curl --fail-with-body \
  --request POST \
  --header 'Content-Type: application/json' \
  --data '{"id":"ord_42","status":"paid","buyer_email":"buyer@example.com","buyer_name":"Riley","seller_name":"Northwind Prints","total_cents":2400,"currency":"usd","seller_assets":[{"name":"Print pack","download_url":"https://downloads.example.com/orders/ord_42"}]}' \
  http://localhost:8080/orders/handoff
