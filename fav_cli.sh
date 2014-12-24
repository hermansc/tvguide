#!/bin/bash

usage(){
  echo "$0 (insert|delete) value";
}

# Get DB password
PASSWORD=""
while read line
do
  read key value <<<$(IFS="="; echo $line)
  if [ "$key" = "dbPass" ]; then
    PASSWORD="$value"
  fi
done < app.conf

# Parse command line
if [ $# -lt 2 ]; then
  usage;
  exit 2;
fi

# Check if we define channel regex
CREGEX=".*"
if [ -n $3 ]; then
  CREGEX=$3;
fi

# Get command
CMD=$1
if [ "$CMD" = "insert" ]; then
  Q="INSERT INTO tvguide_favorites(regex, channel_regex) VALUES('$2', '$CREGEX')"
elif [ "$CMD" = "delete" ]; then
  Q="DELETE FROM tvguide_favorites WHERE id='$2'";
elif [ "$CMD" = "select" ]; then
  Q="SELECT * FROM tvguide_favorites WHERE regex ILIKE '%$2%'"
else
  usage;
  exit 2;
fi

export PGPASSWORD="$PASSWORD";
psql -d tvguide tvguide_user -c "$Q"
