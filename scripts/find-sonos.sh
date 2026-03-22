#!/usr/bin/env bash
# Find Sonos One on the network
echo "=== Searching for Sonos devices ==="
echo ""

# Method 1: nmap (fast)
if command -v nmap &>/dev/null; then
  echo "Scanning with nmap..."
  SUBNET=$(ip route | grep -oP "\d+\.\d+\.\d+\.\d+/\d+" | head -1)
  nmap -p 1400 --open "$SUBNET" -oG - 2>/dev/null | grep "1400/open" | awk '{print $2}'
fi

echo ""
echo "Method 2: Check device (expects Sonos at found IP):"
for ip in $(arp -a 2>/dev/null | awk '{print $2}' | tr -d '()'); do
  result=$(curl -s --max-time 1 "http://$ip:1400/xml/device_description.xml" 2>/dev/null)
  if echo "$result" | grep -q "Sonos"; then
    name=$(echo "$result" | grep -oP "(?<=<friendlyName>)[^<]+" | head -1)
    model=$(echo "$result" | grep -oP "(?<=<modelName>)[^<]+" | head -1)
    echo "  FOUND: $ip — $name ($model)"
  fi
done

echo ""
echo "Set: export SONOS_IP=<ip>"
echo "Then: make run-sonos-ip"
