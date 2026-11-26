def untyped_reader(request):
    return request.headers.get("X-Plain")
