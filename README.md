Basic App

- basic-ws-server : websocket server with basic functionality
- basic-ws-client : websocket cloent with basic functionality

```
# aws login to docker 
aws ecr get-login-password --region us-west-2 --profile=<profile-name> | docker login --username AWS --password-stdin <account_id>.dkr.ecr.us-west-2.amazonaws.com

docker buildx build --platform linux/amd64 -t shailxeta/basic-ws-server:latest .
docker tag shailxeta/basic-ws-server:latest <account_id>.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-server:latest
docker tag shailxeta/basic-ws-server:latest <account_id>.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-server:v1
docker push <account_id>.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-server:latest
docker push <account_id>.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-server:v1

docker buildx build --platform linux/amd64 -t shailxeta/basic-ws-client:latest .
docker tag shailxeta/basic-ws-client:latest <account_id>.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-client:latest
docker tag shailxeta/basic-ws-client:latest <account_id>.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-client:v1
docker push <account_id>.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-client:latest
docker push <account_id>.dkr.ecr.us-west-2.amazonaws.com/shailxeta/basic-ws-client:v1
```
