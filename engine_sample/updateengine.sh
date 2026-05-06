#!/bin/bash

dirs=(
/data/qmserverother/apps/allServer
/data/qmserverother/apps/fightServer
/data/qmserverother/apps/hotServer
/data/qmserverother/apps/robotServer
/data/qmserverother/code/fight
/data/qmserverother/code/hot
/data/qmserverother/code/robot
/data/qmserverother/code/static
/data/qmserverother/plugins/Lib
/data/qmserverother/plugins/LibFight
/data/qmserverother/plugins/LibHot
)

for d in "${dirs[@]}"; do
  echo ">>> Entering $d"
  cd "$d" || continue
  go get tzgit.kaixinxiyou.com/engine/hf@stable_qm
  go get tzgit.kaixinxiyou.com/utils/common@main
  go get tzgit.kaixinxiyou.com/utils/tzid@main
  go get tzgit.kaixinxiyou.com/utils/common/define@main
  go get tzgit.kaixinxiyou.com/engine/enginemessage@main
  go get tzgit.kaixinxiyou.com/utils/versioninfo@main
  go get github.com/redis/go-redis/v9@v9.10.0
  go get github.com/cespare/xxhash/v2@v2.3.0
	go get github.com/redis/go-redis/v9@v9.10.0
	go get go.uber.org/multierr@v1.11.0 
	go get go.uber.org/zap@v1.27.0
	go get golang.org/x/net@v0.40.0 
	go get golang.org/x/sys@v0.33.0 
	go get golang.org/x/text@v0.25.0 
	go get golang.org/x/time@v0.11.0 
  go mod tidy
done
cd /data/qmserverother/bin