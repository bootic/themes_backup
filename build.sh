# https://www.digitalocean.com/community/tutorials/how-to-build-go-executables-for-multiple-platforms-on-ubuntu-16-04
mkdir -p builds
env GOOS=linux GOARCH=386 go build -o builds/bootic_themes_backup.linux386
