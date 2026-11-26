using Microsoft.AspNetCore.Mvc;

namespace Sample;

[ApiController]
[Route("api/items")]
public class ItemsController : ControllerBase {
    [HttpGet("{id}")]
    public ActionResult<string> Get(int id) {
        return Ok($"item-{id}");
    }

    [HttpPost]
    public IActionResult Create([FromBody] string name) {
        return Created($"/api/items/{name}", name);
    }
}
