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
if [ $# -ne 2 ]; then
  usage;
  exit 2;
fi

# Get command
CMD=$1
if [ "$CMD" = "insert" ]; then
  Q="INSERT INTO tvguide_favorites(regex) VALUES('$2')"
elif [ "$CMD" = "delete" ]; then
  Q="DELETE FROM tvguide_favorites WHERE id='$2'";
elif [ "$CMD" = "select" ]; then
  Q="SELECT * FROM tvguide_favorites WHERE regex='$2'"
else
  usage;
  exit 2;
fi

export PGPASSWORD="$PASSWORD";
psql -d tvguide tvguide_user -c "$Q"
