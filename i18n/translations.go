package i18n

// Lang represents a supported language
type Lang string

const (
	EN Lang = "en"
	PT Lang = "pt"
)

// Translations holds all translated strings for a language
type Translations struct {
	// Navigation
	NavHome  string
	NavPosts string
	NavAbout string

	// Posts page
	PostsTitle       string
	SearchPlaceholder string
	SearchButton     string
	NoPostsFound     string

	// Footer
	FooterCopyright string

	// Home page
	HeroTitle    string
	HeroRole     string
	HeroTagline  string
	HeroBio      string

	// Tech categories
	TechBackendTitle string
	TechBackendDesc  string
	TechFrontendTitle string
	TechFrontendDesc  string
	TechInfraTitle    string
	TechInfraDesc     string

	// About page
	AboutTitle   string
	AboutIntro   string
	AboutBlog    string
	AboutContact string
	AboutContactLink string
}

var translations = map[Lang]Translations{
	EN: {
		// Navigation
		NavHome:  "Home",
		NavPosts: "Posts",
		NavAbout: "About",

		// Posts page
		PostsTitle:        "Posts",
		SearchPlaceholder: "Search posts...",
		SearchButton:      "Search",
		NoPostsFound:      "No posts found for",

		// Footer
		FooterCopyright: "Paulo Vicente. All rights reserved.",

		// Home page
		HeroTitle:   "Paulo Vicente",
		HeroRole:    "Full-Stack Software Engineer",
		HeroTagline: "Building scalable, resilient systems with Golang and modern distributed architectures",
		HeroBio:     "I'm a full-stack Software Engineer with backend expertise and a passion for building scalable, resilient systems using Golang and modern distributed architectures. With years of experience designing high-performance backends—leveraging Go's concurrency model, efficient memory management, and compile-time safety—alongside modern TypeScript/React frontends, I specialize in turning challenging business requirements into efficient, maintainable end-to-end solutions.",

		// Tech categories
		TechBackendTitle:  "Backend & Architecture",
		TechBackendDesc:   "Expert in Golang, microservices, RESTful APIs, gRPC, and event-driven architectures with Kafka and RabbitMQ. Experienced with CQRS, Event Sourcing, Domain-Driven Design (DDD), and scalable system design patterns",
		TechFrontendTitle: "Frontend & UX",
		TechFrontendDesc:  "Proficient in React, TypeScript, Redux, and building responsive, accessible user experiences",
		TechInfraTitle:    "Infrastructure & DevOps",
		TechInfraDesc:     "Hands-on with Docker, Kubernetes, AWS, CI/CD pipelines, and observability tools",

		// About page
		AboutTitle:       "About Me",
		AboutIntro:       "Hi! I'm Paulo Vicente, a software developer passionate about programming and technology.",
		AboutBlog:        "This blog is where I share my thoughts, experiences, and learnings about software development, programming languages, and various technical topics that interest me.",
		AboutContact:     "Get in Touch",
		AboutContactLink: "Feel free to reach out to me on",
	},
	PT: {
		// Navigation
		NavHome:  "Início",
		NavPosts: "Posts",
		NavAbout: "Sobre",

		// Posts page
		PostsTitle:        "Posts",
		SearchPlaceholder: "Pesquisar posts...",
		SearchButton:      "Pesquisar",
		NoPostsFound:      "Nenhum post encontrado para",

		// Footer
		FooterCopyright: "Paulo Vicente. Todos os direitos reservados.",

		// Home page
		HeroTitle:   "Paulo Vicente",
		HeroRole:    "Engenheiro de Software Full-Stack",
		HeroTagline: "Construindo sistemas escaláveis e resilientes com Golang e arquiteturas distribuídas modernas",
		HeroBio:     "Sou um Engenheiro de Software full-stack com especialização em backend e paixão por construir sistemas escaláveis e resilientes usando Golang e arquiteturas distribuídas modernas. Com anos de experiência projetando backends de alto desempenho—aproveitando o modelo de concorrência do Go, gerenciamento eficiente de memória e segurança em tempo de compilação—junto com frontends modernos em TypeScript/React, me especializo em transformar requisitos de negócio desafiadores em soluções end-to-end eficientes e mantíveis.",

		// Tech categories
		TechBackendTitle:  "Backend & Arquitetura",
		TechBackendDesc:   "Especialista em Golang, microsserviços, APIs RESTful, gRPC e arquiteturas orientadas a eventos com Kafka e RabbitMQ. Experiente com CQRS, Event Sourcing, Domain-Driven Design (DDD) e padrões de design de sistemas escaláveis",
		TechFrontendTitle: "Frontend & UX",
		TechFrontendDesc:  "Proficiente em React, TypeScript, Redux e construção de experiências de usuário responsivas e acessíveis",
		TechInfraTitle:    "Infraestrutura & DevOps",
		TechInfraDesc:     "Experiência prática com Docker, Kubernetes, AWS, pipelines CI/CD e ferramentas de observabilidade",

		// About page
		AboutTitle:       "Sobre Mim",
		AboutIntro:       "Olá! Sou Paulo Vicente, um desenvolvedor de software apaixonado por programação e tecnologia.",
		AboutBlog:        "Este blog é onde compartilho meus pensamentos, experiências e aprendizados sobre desenvolvimento de software, linguagens de programação e vários tópicos técnicos que me interessam.",
		AboutContact:     "Entre em Contato",
		AboutContactLink: "Fique à vontade para me contatar no",
	},
}

// Get returns the translations for the given language
func Get(lang Lang) Translations {
	if t, ok := translations[lang]; ok {
		return t
	}
	return translations[EN] // Default to English
}

// GetLang parses a language string and returns the corresponding Lang
func GetLang(s string) Lang {
	switch s {
	case "pt":
		return PT
	default:
		return EN
	}
}

// SupportedLanguages returns all supported languages
func SupportedLanguages() []Lang {
	return []Lang{EN, PT}
}

// OtherLang returns the other language (for language switcher)
func OtherLang(lang Lang) Lang {
	if lang == EN {
		return PT
	}
	return EN
}

// LangName returns the display name for a language
func LangName(lang Lang) string {
	switch lang {
	case PT:
		return "Português"
	default:
		return "English"
	}
}

// LangCode returns the short code for a language
func LangCode(lang Lang) string {
	return string(lang)
}
