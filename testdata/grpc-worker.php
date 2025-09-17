<?php

// Handler outside the loop for better performance (doing less work)
$handler = static function (array $request): array  {
	// Do something with the request

    return ['message' => "Hello, {$request['Name']}"];
};

$maxRequests = (int)($_SERVER['MAX_REQUESTS'] ?? 0);
for ($nbRequests = 0; !$maxRequests || $nbRequests < $maxRequests; ++$nbRequests) {
    $keepRunning = \frankenphp_handle_request($handler);

    // Call the garbage collector to reduce the chances of it being triggered in the middle of the handling of a request
    gc_collect_cycles();

    if (!$keepRunning) {
      break;
    }
}
