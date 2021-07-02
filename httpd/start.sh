#!/bin/bash

gulp

export DEBUG=1
export RPC="localhost:8901"
export CONFIG_FILE=/srv/ff/config.json
gin -p 8080
