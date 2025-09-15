/* This is a generated file, edit the .stub.php file instead.
 * Stub hash: 62eed377d4b8f6aae7276363fe7a58c3ecda23af */

ZEND_BEGIN_ARG_WITH_RETURN_TYPE_INFO_EX(arginfo_grpc_get_request, 0, 0,
                                        IS_ARRAY, 0)
ZEND_END_ARG_INFO()

ZEND_BEGIN_ARG_WITH_RETURN_TYPE_INFO_EX(arginfo_grpc_send_response, 0, 1,
                                        IS_VOID, 0)
ZEND_ARG_TYPE_INFO(0, response, IS_ARRAY, 0)
ZEND_END_ARG_INFO()

ZEND_FUNCTION(grpc_get_request);
ZEND_FUNCTION(grpc_send_response);

static const zend_function_entry ext_functions[] = {
    ZEND_FE(grpc_get_request, arginfo_grpc_get_request)
        ZEND_FE(grpc_send_response, arginfo_grpc_send_response) ZEND_FE_END};
