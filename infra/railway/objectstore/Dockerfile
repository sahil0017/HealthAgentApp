# SeaweedFS S3 gateway for Railway: the start command is baked in because Railway images cannot take a command from the CLI.
FROM chrislusf/seaweedfs:latest
CMD ["server", "-s3", "-s3.port=9000", "-dir=/data", "-ip.bind=0.0.0.0"]
