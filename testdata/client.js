// Import the gRPC-Web library
const { GreeterClient } = require('./helloworld_grpc_web_pb.js');
const { HelloRequest } = require('./helloworld_pb.js');

// Create the gRPC client, pointing to your Caddy server
const client = new GreeterClient('https://localhost');

// This function will be called by the button in the HTML.
// We attach it to the window object to make it globally accessible.
window.greet = function() {
    const request = new HelloRequest();
    const name = document.getElementById('nameInput').value;
    request.setName(name);

    const responseDiv = document.getElementById('response');
    responseDiv.textContent = 'Sending...';

    client.sayHello(request, {}, (err, response) => {
        if (err) {
            responseDiv.textContent = 'Error: ' + err.message;
            console.error('gRPC Error:', err);
            return;
        }
        responseDiv.textContent = response.getMessage();
    });
}

