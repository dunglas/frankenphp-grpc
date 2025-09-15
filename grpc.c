#include "grpc.h"
#include "grpc_arginfo.h"
#include <ext/standard/php_var.h>
#include <php.h>

#include "_cgo_export.h"

PHP_FUNCTION(grpc_get_request) {
  if (zend_parse_parameters_none() == FAILURE) {
    return;
  }

  zend_is_auto_global_str(ZEND_STRL("_SERVER"));
  zval *server_vars = &PG(http_globals)[TRACK_VARS_SERVER];

  zval *http_id_header =
      zend_hash_str_find(Z_ARRVAL_P(server_vars), "HTTP_ID", strlen("HTTP_ID"));
  zval *request = go_get_request(Z_STR_P(http_id_header));

  RETURN_ZVAL(request, 0, 0);
}

PHP_FUNCTION(grpc_send_response) {
  zval *response;

  ZEND_PARSE_PARAMETERS_START(1, 1)
  Z_PARAM_ARRAY(response)
  ZEND_PARSE_PARAMETERS_END();

  zval *server_vars = &PG(http_globals)[TRACK_VARS_SERVER];
  zval *http_id_header =
      zend_hash_str_find(Z_ARRVAL_P(server_vars), "HTTP_ID", strlen("HTTP_ID"));

  go_send_response(Z_STR_P(http_id_header), response);
}

zend_module_entry ext_module_entry = {STANDARD_MODULE_HEADER,
                                      "frankenphp_grpc",
                                      ext_functions, /* Functions */
                                      NULL,          /* MINIT */
                                      NULL,          /* MSHUTDOWN */
                                      NULL,          /* RINIT */
                                      NULL,          /* RSHUTDOWN */
                                      NULL,          /* MINFO */
                                      "0.1.0",
                                      STANDARD_MODULE_PROPERTIES};
