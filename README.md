# WORK IN PROGRESS

Do not touch. Slippery. Wet. Harmful. Not for oral consumption.

# zkonce

Zookeeper Once - run a command once per given timeframe.

`zkonce` runs `${cmd}` if `${cmd}` was not already run this hour,
this day or this side of infinity. If it has, it exits cleanly
without executing `${cmd}`.

The time interval is either measured from the start or the finish
of the previous run.

# Zookeeper layout

```
/
└── zkonce
    └── <syncgroup>
        └── <job>
            ├── attempt
            ├── start
            ├── finish
            └── runlock/
```

# Configuration

```
/etc/zkonce/zkonce.conf:
ensemble: <zk-connect-string>
syncgroup: <name>
```

# Execute

```
zkonce --job <name> --per day|hour|inf --from-start -- ${cmd}
zkonce --job <name> --per day|hour|inf --from-finish -- ${cmd}
zkonce --job <name> --per inf --barrier /run/.zkonce.done -- ${cmd}
```
