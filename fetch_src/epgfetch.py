#!/usr/bin/env python
from lxml import objectify
from datetime import datetime, timedelta
from dateutil import parser
import json, urllib2, time, gzip, StringIO, psycopg2, sys, pytz, os

def main(config):
    # Cache or something for each channel, so we dont do dupliacte requests.
    channel_cache = {}

    channels = [
            { 'epg': 'aljazeera.net', 'ui': 'Al Jazeera'},
            { 'epg': 'nrk1.nrk.no',   'ui': 'NRK1'},
            { 'epg': 'nrk2.nrk.no',   'ui': 'NRK2'},
            { 'epg': 'nrk3.nrk.no',   'ui': 'NRK3'},
            { 'epg': 'film.tv2.no',   'ui': 'TV2 Film'},
            { 'epg': 'bliss.tv2.no',  'ui': 'TV2 Bliss'},
            { 'epg': 'tv2.no',        'ui': 'TV2'},
            { 'epg': 'news.tv2.no',   'ui': 'TV2 Nyheter'},
            { 'epg': 'sport.tv2.no',  'ui': 'TV2 Sport'},
            { 'epg': 'pl1.tv2.no',    'ui': 'TV2 Premium'},
            { 'epg': 'pl2.tv2.no',    'ui': 'TV2 Premium2'},
            { 'epg': 'pl3.tv2.no',    'ui': 'TV2 Premium3'},
            { 'epg': 'zebra.tv2.no',  'ui': 'TV2 Zebra'},
            { 'epg': 'max.no',        'ui': 'MAX'},
            { 'epg': 'tvnorge.no',    'ui': 'TV Norge'},
            { 'epg': 'fotball.cmore.no', 'ui': 'C More Fotball'},
            { 'epg': 'viasat4.no',    'ui': 'Viasat 4'},
            { 'epg': 'tv3.no',        'ui': 'TV3'},
            { 'epg': 'voxtv.no',      'ui': 'VOX'},
            { 'epg': 'fem.no',        'ui': 'FEM'},
            # { 'epg': 'no.bbchd.no',   'ui': 'BBC World News'},
            # { 'epg': 'pl1.tv2.no',    'ui': 'TV2 Premium HD'},
            # { 'epg': 'pl2.tv2.no',    'ui': 'TV2 Premium2 HD'},
            # { 'epg': 'pl3.tv2.no',    'ui': 'TV2 Premium3 HD'},
            # { 'epg': 'supertv.nrk.no', 'ui': 'NRK Super'},
            # { 'epg': 'cnn.com',       'ui': 'CNN International'},
    ]

    base = "http://xmltv.xmltv.se" if config["fetchModus"] == "xml.gz" else "http://json.xmltv.se"
    dates = [datetime.today() + timedelta(days=i) for i in range(0,int(config["fetchDays"]))]
    conn = psycopg2.connect("dbname=%s user=%s password=%s" % (config["dbName"], config["dbUser"], config["dbPass"]))
    cur = conn.cursor()

    for channel in channels:
        print "Getting events for %s" % (channel["ui"])

        # Delete all existing data for this channel in epg-db.
        cur.execute("DELETE FROM " + config["dbTable"] + " WHERE channel=%s", (channel["ui"],))
        conn.commit()

        progs = []
        for date in dates:
          # Get from cache, or add it to cache if not found.
          channel_key = channel["epg"] + "_" + date.strftime("%Y-%m-%d")
          if channel_key not in channel_cache:
            try:
              resp = urllib2.urlopen("%s/%s.%s" % (base,channel_key,config["fetchModus"]))
            except Exception as exp:
              print "Got exception fetching %s" % channel_key
              raise(exp)

            response = ""
            while 1:
              data = resp.read()
              if not data:
                break
              response += data

            try:
              compr = StringIO.StringIO()
              compr.write(response)
              compr.seek(0)
              f = gzip.GzipFile(fileobj=compr, mode='rb')
              channel_cache[channel_key] = f.read()
            except IOError as e:
              channel_cache[channel_key] = response

          progs.extend(parse_channel(channel_cache[channel_key], channel, mode=config["fetchModus"]))
          time.sleep(0.05)

        # Insert all found programmes, with a multi-insert.
        insert_vals = []
        if not progs:
            print "No events in %s" % (channel["ui"])
            continue
        for p in progs:
          insert = (p["start"],p["stop"],p["title"].strip(),channel["ui"],p["description"].strip())
          insert_vals.append(insert)
        args_str = ",".join(cur.mogrify("(%s,%s,%s,%s,%s)", x) for x in insert_vals)
        cur.execute("INSERT INTO " + config["dbTable"] + "(start,stop,title,channel,description) VALUES" + args_str)
        print "Inserted %d events to %s" % (len(progs), channel["ui"])
    conn.commit()
    cur.close()
    conn.close()

def parse_channel(inp, channel, mode="xml.gz"):
  programmes = []
  if mode == "xml.gz":
    root = objectify.fromstring(inp)
    if not hasattr(root, 'programme'):
        return
    for programme in root["programme"]:
        d = {}
        d["start"] = parser.parse(programme.attrib["start"]).isoformat()
        d["stop"] = parser.parse(programme.attrib["stop"]).isoformat()
        d["title"] = unicode(programme["title"])
        d["description"] = unicode(programme["desc"]) if hasattr(programme, "desc") else ""
        programmes.append(d)
  elif mode == "js.gz":
    root = json.loads(inp)["jsontv"]
    if not "programme" in root:
      return
    for programme in root["programme"]:
      d = {}

      # Convert times from epoch.
      d["start"] = pytz.utc.localize(datetime.fromtimestamp(int(programme["start"]))).isoformat()
      d["stop"] = pytz.utc.localize(datetime.fromtimestamp(int(programme["stop"]))).isoformat()

      # They differientiate betwen english and norwegian titles. We preffer norwegian.
      titles = programme.get("title", {})
      d["title"] = unicode(titles.get("no")) if titles.get("no") else unicode(titles.get("en", ""))

      # Same with descriptions.
      descriptions = programme.get("desc", {})
      d["description"] = unicode(descriptions.get("no")) if descriptions.get("no") else unicode(descriptions.get("en", ""))
      programmes.append(d)
  return programmes

def parse_config(filename):
    config = {}
    fh = open(filename, 'r')
    for line in fh:
        c = line.split("=")
        key = c[0].strip()
        value = c[1].strip()
        config[key] = value
    return config

if __name__ == "__main__":
    conf_file = os.path.dirname(os.path.realpath(__file__)) + "/../app.conf"
    if len(sys.argv) > 1:
        conf_file = sys.argv[1]
    config = parse_config(conf_file)
    main(config)
