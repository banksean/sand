container run -d \
  --name apt-cacher-ng \
  -p 3142:3142 \
  --volume ~/.cache/apt-cacher-ng:/var/cache/apt-cacher-ng \
  apt-cacher-ng