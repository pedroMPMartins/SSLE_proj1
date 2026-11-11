#!/usr/bin/env python3
"""
SSLE Guide 4: Convert AlienVault IP Reputation List to Wazuh CDB format.
Usage:
    python3 generate_cdb.py [input_file] [output_file]
"""

import sys
import os

def convert_ip_list(input_path, output_path):
    if not os.path.exists(input_path):
        print(f"Error: input file {input_path} does not exist.")
        sys.exit(1)

    valid_entries = 0
    with open(input_path, 'r') as infile, open(output_path, 'w') as outfile:
        for line in infile:
            line = line.strip()
            if not line or line.startswith('#'):
                continue

            if ':' in line:
                ip, desc = line.split(':', 1)
                ip = ip.strip()
                desc = desc.strip()
            else:
                ip = line
                desc = "malicious"

            outfile.write(f"{ip}:{desc}\n")
            valid_entries += 1

    print(f"Successfully converted {valid_entries} IP reputation entries into {output_path}")

if __name__ == "__main__":
    in_file = sys.argv[1] if len(sys.argv) > 1 else "alienvault_reputation"
    out_file = sys.argv[2] if len(sys.argv) > 2 else "alienvault_reputation.cdb_input"
    convert_ip_list(in_file, out_file)
