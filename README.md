# VISC

`visc` 是一个用于生成结构体字段 `getter`/`setter` 方法、以及生成结构体构造方法的代码生成工具。虽然像 Goland 这样的 IDE 也提供 `getter`/`setter` 方法生成功能，但 `visc` 拥有更多自定义配置的生成方式，并且支持生成快速构造结构体字段值的构造方法。

## 使用方式

`visc` 需要配合 `go generate` 命令使用，在你希望生成 `getter`/`setter` 方法的包内，在任意一个 `.go` 文件中加入以下 `go generate` 命令：

```go
//go:generate visc [file]...
```

`visc` 命令将默认扫描包内所有的结构体，并为目标结构体（目标结构体指：字段 tag、即 StructTag 中有对应 `getter`/`setter` 标签 的结构体，或在结构体的文档注释中包含 `visc` 指令的结构体）生成相应的 `getter`/`setter` 方法。可以通过可选的位置参数 `file` 来指定只扫描特定文件内的结构体。额外的，`visc` 仅扫描可导出的（Exported）的结构体，私有的结构体将不会生成 `getter`/`setter`。

~~***注：`visc` 仅支持在非 `main` 包中使用，这是由于其在生成代码的过程中，会生成中间代码并引入目标包的结构体类型，而 `main` 包不支持被导入。***~~ 

***更新：从 v0.2 版本开始，通过使用静态代码分析，visc 也支持在 `main` 包中使用。*** 

### visc 指令

```go
// visc:all(getter=true, getPrefix=Get, setter=true, setPrefix=Set)
type T struct {
  Field string
}
```

`visc:all` 指令用于为所有 StructFields 生成 `getter/setter` 而不需要单独为每个字段添加 StructTag，使用 `getter=true` 和 `setter=true` 来指定是否生成 `getter/setter`，使用 `getPrefix=Get` 和 `setPrefix=Set` 来指定生成的 `getter/setter` 应该带有何种前缀；在上述例子中，生成的 `getter` 和 `setter` 分别为 `GetField` 和 `SetField`。

额外的，如果结构体中某个字段已经设置了 `getter`/`setter` StructTag，则优先使用 StructTag 的内容生成对应的 `getter`/`setter`，如果 StructTag 的内容为 `-`，则即使配置了 `visc:all` 指令，也不会生成该字段对应的 `getter`/`setter`。

````go
// visc:construct(name=construct, prefix=Get)
type T struct {
  Field string `setter:"setField"`
}
````

`visc:construct` 指令用于生成该结构体的构造方法，生成结果如下所示：

````go
func (instance *T) construct(constructor interface {
	GetField() string
}) *T {
	instance.setField(constructor.GetField())
	return instance
}
````

`visc:construct` 仅会对拥有 `setter` 方法的字段生成对应的赋值代码，即上述例子中的 `instance.setField(constructor.GetField())`。

*作者注：生成这样的构造方法有什么用？这是源于我在 DDD（领域驱动设计）的实践中，困扰于 DDD 各层级之间数据交互需要频繁地在各种 DTO 之间进行转换，而这些 DTO 在结构上又非常相似（甚至可以说大部分 DTO 是完全一致的），我常常需要写很多 DTO 之间转换拷贝赋值的代码，这非常花时间。这也是 `visc@v0.2` 新增特性的起因，通过代码静态分析生成结构体的构造方法，这个构造方法接收一个接口类型，该接口类型定义了一系列 `getter` 方法，通过 get 值并 set 的方式完成结构体的转换拷贝赋值，而实现这个接口类型的结构体，也可以由 `visc` 完成生成对应 `getter` 的操作，这极大地提高了 DTO 转换的效率。*

### StructTag: getter

一个基本的 `getter` tag 示例如下：

```go
type User struct {
  id   int64  `getter:"GetID"`
  name sql.NullString `getter:"*,ref"`
}
```

将生成如下的 `getter` 方法：

```go
type (instance *User) GetID() int64 { return instance.id }
type (instance *User) Name() *sql.NullString { return &instance.name }
```

使用 `getter:"*"` 来指定一个字段需要生成 `getter` 方法，`*` 表示以默认的 `CamelCase` 格式生成 `getter` 方法，例如 `name` 字段将生成 `Name` 方法，你也可以自定义 `getter` 方法名，将 `*` 替换成想要生成的 `getter` 方法名即可，例如：`getter:"GetID"`，将生成 `GetID` 方法。

额外的，如果某个字段是一个体积较大的结构体，直接返回会发生较大的拷贝开销，那么可以通过 `ref` 来指定返回其指针，例如上述例子中的 `getter:"*,ref"` 将返回 `name` 字段的引用。

### StructTag: setter

`setter` tag 与 `getter` tag 用法大体相同：`setter:"*"`，同样支持将 `*` 替换成想要生成的 `setter` 方法名，但需要注意的是，`setter` 默认生成的方法名为 `Set + 字段名`，而 `getter` 默认生成的方法名则没有 `Get` 前缀。特别的，`setter` tag 不支持 `ref` 引用模式，所有 `setter` 方法都对应值类型。

```go
type User struct {
  id int64
  name sql.NullString `setter:"*"`
}

type (instance *User) SetName(value sql.NullString) { instance.name = value }
```

从 `v0.3.0` 版本开始，`visc` 新增了 `construct` StructTag，其作用是当你想自定义 constructor 的 `Set` 方法时，你可以像这样来指定不生成/使用默认的 `setter`，而是使用你自定义的 `setter`：

```go
type User struct {
  id int64
  name sql.NullString `setter:"-" construct:"MySetName(string)"`
}

// User defined Method
type (user *User) MySetName(name string) { 
    instance.name = sql.NullString{
        Valid:  true,
        String: name,
    } 
}
```

其格式为 `$METHOD($TYPE)`，其中，`$METHOD` 是用户自定义的方法名，`$TYPE` 是参数类型。

### proxy 模式（已移除）

**Deprecated: 从 `v0.2` 开始，visc 不再支持 proxy 模式，如果需要使用 proxy，请酌情使用 `v0.1` 版本的 visc；实际上，proxy 模式在实际的编码场景并不常见，应该考虑使用其他方式代替 proxy。**

使用 `getter:",proxy=Name Age"` 来代理结构体中对应字段下的 `Name` 字段（`setter` 同理），例如：

```go
type User struct {
  id int64
  name sql.NullString `getter:",proxy=String Valid"`
}

// 将生成如下 getter 方法
type (instance *User) String() string { return instance.name.String }
type (instance *User) Valid() bool { return instance.name.Valid }
```

`proxy` 应是 `getter`/`setter` tag 的至少第二个参数（第一个参数为 `*` 或方法名），其语法为 `proxy=field1 field2 ...`，其中 `field` 指的是需要代理的字段名称，多个字段用空格分隔，`getter` 方法将默认以该字段名称作为方法名，`setter` 方法将默认以 `Set + 字段名称` 作为方法名；如果需要指定 `getter`/`setter` 方法名，则可以使用这样的语法：`proxy=Field:Method`，其中 `Filed` 为字段名，`Method` 为定义的方法名称。

额外的，对于 `getter` 方法，如果需要使用引用（指针）类型，可以使用这样的语法：`proxy=*Field`，在字段名称前添加 `*` 号，同样适用于指定方法名的场景，如：`proxy=*Field:Method`。

### 自动包引入

由于 `visc` 会生成中间代码并使用 `reflect` 扫描结构体，因此你无需担心包引入的问题，`visc` 会自动将需要引入的包引入并正确处理导入的类型，例如，当你的代码使用了 `sql.NullString` 类型时，`visc` 会自动导入 `database/sql` 包。

**从 `v0.2` 版本开始，自动包引入功能由生成中间代码并使用 `reflect` 扫描结构体的方式，更改为静态代码分析的方式，静态代码分析不仅效率更高，并且限制更少，对泛型的兼容性更好。**

### 关于泛型

`visc` 对于泛型的支持尚处于实验性阶段，目前已支持对包含泛型的结构体生成 `getter`/`setter`，也支持为类型包含泛型参数的字段生成 `getter`/`setter`。

## 参数

```
Usage of visc:
  -buildtags
    	生成的文件所携带的 build tags（注意，格式应为 // +build 指令的格式，而非 //go:build 指令的格式）
  -output
    	指定生成的文件名称，默认为 "visc.gen.go"
  -version
    	visc version
```

## 使用场景

其实 Go 官方并不提倡也不遏制对 `getter`/`setter` 方法的使用，大多数场合下都推荐使用可导出的字段；但某些特定场景下，对于只读值对象，我们不希望使用者修改其字段值，则可以通过 private 字段配合 `getter` 方法实现。

一个实际应用的例子是，对于通过 json 反序列化生成的对象，我们希望其只读但不可写，那么通过 `visc` 配合 `easyjson` 生成对应字段的 `getter` 及私有字段的反序列化方法是一个（我认为）比较好的实现方式（为此我 fork 了 `easyjson` 并修改了代码使其支持生成私有字段的 `MarshalJSON`/`UnmarshalJSON` 方法）。

## 致谢

`visc@v0.1` 在中间代码生成及对于导入类型及包的处理上借鉴了 [easyjson](https://github.com/mailru/easyjson) 相关代码，特此鸣谢。