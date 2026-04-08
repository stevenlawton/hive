#!/bin/bash
cd "$(dirname "$0")" && go build -o hive . && exec ./hive
