def include_from_test(app, router):
    app.include_router(router, prefix='/t011g/body')
