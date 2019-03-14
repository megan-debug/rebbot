# Use the latest perl image from dockerhub
FROM golang:latest

EXPOSE 8080

# add your application code and set the working directory
ADD . /app
WORKDIR /app

# change the permissions and run the application
ENV GOPATH /app/vendor
CMD ["go", "run", "main.go"]
