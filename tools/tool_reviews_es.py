#!/usr/bin/env python3
"""Spanish for the method reviews in tool_reviews.py.

Keyed by tool name. A tool with an English review and no entry here falls back
to the English, which keeps the Spanish transcript usable while it is behind
rather than leaving a gap where the judgement should be.

The scores are not repeated: a number does not need translating, and duplicating
it would be one more place for the two files to disagree.
"""

REVIEWS_ES = {}


def review(tool, did, better):
    REVIEWS_ES[tool] = {"did": did, "better": better}


# --- ver el escritorio --------------------------------------------------------

review(
    "get_desktop_info",
    "Leyó display, encoder, resolución, gestor de ventanas, uptime y memoria en "
    "una sola llamada, desde /proc y la configuración en uso en vez de "
    "preguntarle a X.",
    "Mezcla datos que nunca cambian (display, gestor de ventanas) con otros que "
    "cambian cada segundo (memoria, carga). Quien haga polling de la memoria "
    "vuelve a leer las constantes cada vez. Habría que separar la mitad volátil, "
    "o decir cuándo se tomó cada campo.",
)
review(
    "get_screen_info",
    "Reportó la geometría y la cantidad de escritorios virtuales directo desde "
    "la conexión X.",
    "Ya es la fuente primaria: le pregunta a X, no parsea nada. Sólo mejoraría "
    "reportando la geometría por monitor, que este Xvfb de una sola cabeza no "
    "tiene forma de tener.",
)
review(
    "screenshot",
    "Capturó el framebuffer de forma nativa por desktop.GrabScreenshotPNG y "
    "devolvió PNG, con la misma captura disponible inline, a archivo, o "
    "empujada al navegador que esté mirando.",
    "Sin subproceso, sin pérdida por compresión, y un solo camino de captura "
    "para tres destinos. Para ir más lejos necesitaría damage tracking —"
    " devolver sólo lo que cambió desde la última llamada — que en un "
    "escritorio mayormente quieto recortaría un orden de magnitud los bytes que "
    "lee un modelo.",
)
review(
    "screenshot_region",
    "Recortó en el momento de capturar en vez de traer la pantalla y cortarla, "
    "así el costo es el rectángulo y no el display.",
    "La forma ya es la correcta. La mejora está en el nivel del que llama: "
    "combinarla con ui_find para poder nombrar una región ('el diálogo') en vez "
    "de medirla.",
)
review(
    "get_pixel_color",
    "Leyó un pixel por la conexión X — la forma más barata posible de verificar "
    "estado, sin que cruce ninguna imagen.",
    "Nada que mejorar en el mecanismo. Lo útil sería una compañera que espere a "
    "que un pixel cambie, lo que convertiría la lectura más barata en la "
    "primitiva de sincronización más barata.",
)
review(
    "read_screen_text",
    "Leyó la pantalla desde el árbol de accesibilidad donde la aplicación "
    "expone uno, cayó al OCR donde no lo hay, y dijo cuál contestó.",
    "El OCR ahora es el respaldo y no el método, que es toda la diferencia. "
    "Sobre la misma pantalla el árbol devolvió cada etiqueta exacta — "
    "Minimize, Restore, Tab search, Bookmark this tab, Save changes — mientras "
    "tesseract devolvía 'mel OF Se @ vread.htm!- chromium', 'GC QQ Q@File', "
    "perdía un guion de --no-sandbox y no encontraba el botón. Quien llama no "
    "puede distinguir una mala lectura de una lectura, así que la respuesta "
    "ahora dice de qué fuente salió y el OCR avisa que es una conjetura. "
    "También se descartan las rachas del carácter de reemplazo de objeto: una "
    "barra de íconos llegaba como una línea de ellos, varias veces por "
    "ventana, gastando tokens sin aportar nada. Lo que falta es alcance — "
    "devuelve todo lo visible, incluido el cromo entero del navegador, sin "
    "forma de pedir sólo la ventana enfocada, y sin agrupación espacial, así "
    "que un diseño a dos columnas se lee como una sola intercalada.",
)
review(
    "find_text",
    "Ubicó texto y devolvió coordenadas de pantalla: exactas desde el árbol de "
    "accesibilidad cuando puede, y cajas de palabra del OCR con confianza "
    "cuando no.",
    "Dos fallas distintas, y la más silenciosa era la peor. El TSV de tesseract "
    "es una fila por palabra, y la búsqueda se probaba contra cada fila por "
    "separado, así que un texto con un espacio no podía coincidir nunca — "
    "'Save changes' y 'Quarterly Report' devolvían 'no match on screen' "
    "estando claramente dibujados, lo que se lee como que el texto no está y "
    "no como que la herramienta sólo sabe buscar de a una palabra. Ahora las "
    "líneas se rearman desde sus palabras, y la caja de una frase es la unión "
    "de las palabras que abarca con la confianza de la más débil, porque una "
    "frase vale lo que su parte menos segura. La otra falla era la fuente: "
    "estas coordenadas van directo a un click, y un carácter mal leído "
    "producía una caja confiada alrededor de otra cosa. Ahora se pregunta "
    "primero al árbol, donde la posición es la que declara la aplicación y "
    "viene marcada como exacta. Queda pendiente: nada verifica que el punto "
    "sea alcanzable, así que una coincidencia debajo de un diálogo se devuelve "
    "igual que una a la vista — la comprobación que browser_click ya hace para "
    "una página.",
)
review(
    "get_mouse_position",
    "Consultó el puntero por la conexión X.",
    "Autoritativo y gratis. No hay nada que agregar.",
)
review(
    "list_processes",
    "Corrió ps y filtró por substring.",
    "Un filtro por substring sobre una tabla de texto encuentra procesos cuyo "
    "argumento apenas menciona el nombre. Leer /proc directo daría coincidencia "
    "exacta sobre comm y argv, más los campos que quien llama suele querer "
    "después — rss, hora de inicio, padre — sin una segunda llamada.",
)
review(
    "is_running",
    "Preguntó si existe un proceso con ese nombre.",
    "Contesta lo que se le preguntó, pero un true/false pelado obliga a correr "
    "list_processes para saber algo más. Devolver la cantidad y los pids no "
    "costaría nada y sacaría esa segunda llamada.",
)
review(
    "list_commands",
    "Listó los ejecutables de PATH agrupados por las secciones del propio "
    "sistema de paquetes, y marcó los que además tienen entrada de escritorio.",
    "Existe porque la mitad de lo instalado no tenía ninguna herramienta. "
    "list_installed_apps lee las entradas .desktop, así que contesta qué hay en "
    "el menú; nada contestaba qué se puede tipear. La única forma de saber si "
    "un comando existía era ejecutar uno, y run_command, terminal_run y "
    "shell_exec son todas riskDanger — así que un agente bajo readonly o safe "
    "podía inventariar el escritorio gráfico y no podía establecer si git "
    "estaba instalado. Leer directorios es una lectura, y clasificarlo así "
    "cierra eso. Las categorías son el campo Section de Debian y no algo "
    "inferido de un nombre, por eso net, vcs y admin significan lo que dicen. "
    "La llamada sin filtro devuelve esos conteos y no 902 nombres, bajo el "
    "principio de que una respuesta que nadie puede leer no es una respuesta. "
    "Las descripciones vienen de dpkg y no de whatis, que era la fuente obvia y "
    "acá no devuelve nada porque la imagen borra las man pages. Lo que no hace "
    "es distinguir un comando de un envoltorio: git-upload-archive aparece al "
    "lado de git con la misma descripción, porque la que la tiene es el "
    "paquete.",
)
review(
    "list_installed_apps",
    "Leyó las entradas .desktop, así que la respuesta es lo que una persona "
    "vería en el menú y no lo que instaló dpkg.",
    "La fuente correcta: lista lo que se puede lanzar, no lo que existe en "
    "disco. Podría traer el ícono y las categorías de cada entrada, que es la "
    "diferencia entre una lista y algo sobre lo que un agente puede razonar.",
)
review(
    "get_audio_state",
    "Le preguntó a PulseAudio por el sink, el volumen y el estado de silencio.",
    "Reporta el sink del que graba el escritorio y no lo que hace el stream de "
    "cada aplicación, así que un agente no puede saber qué programa está "
    "haciendo ruido. Listar los sink inputs contestaría eso.",
)
review(
    "check_errors",
    "Recorrió el árbol de accesibilidad buscando alerts y diálogos, y después "
    "cualquier cosa cuyo texto suene a falla — estructura primero, heurística "
    "después.",
    "El instinto correcto: un programa gráfico no falla con un código de salida, "
    "pone un cuadro en la pantalla. Pero la mitad heurística tiene forma de "
    "inglés, así que un diálogo de error en español o portugués le es invisible "
    "— los mismos tres idiomas que la interfaz ya trae cerrarían eso.",
)
review(
    "wait",
    "Durmió, y desde la etapa 1 corta cuando se cancela la llamada en vez de "
    "dormir a través de la cancelación.",
    "Es la herramienta a la que recurre un modelo cuando no sabe qué está "
    "esperando, y una duración adivinada es o muy corta o desperdiciada. Cada "
    "uso que pueda nombrar una condición debería ser ui_wait_for, "
    "wait_for_window o wait_for_idle. Su descripción debería decirlo.",
)
review(
    "wait_for_idle",
    "Esperó sobre X DAMAGE a que dejaran de pintar la pantalla y la CPU se "
    "calmara, sin capturar nada, y respeta la cancelación.",
    "La respuesta correcta al problema que `wait` adivina, y antes llegaba por "
    "el camino más caro posible: cinco veces por segundo agarraba el framebuffer "
    "entero, lo codificaba en PNG, lo escribía a disco, lo volvía a leer y lo "
    "hasheaba — así que la herramienta para detectar quietud era lo más "
    "ocupado del escritorio mientras corría, y la CPU que gastaba era CPU que "
    "esa misma llamada después citaba como razón de que la máquina no estaba "
    "quieta. X reporta el pintado por DAMAGE, así que ya no captura nada: una "
    "espera de diez segundos pasó de unos 215 procesos extra a la misma cuenta "
    "que una llamada que no hace nada. El número de CPU también estaba mal — "
    "sumaba `ps -eo pcpu`, que es el promedio de cada proceso sobre toda su "
    "vida, así que un daemon ocupado al arrancar seguía inflando el total "
    "mientras viviera; los deltas de /proc/stat miden el intervalo. Queda el "
    "límite que el método no puede arreglar: DAMAGE reporta un reloj que "
    "avanza igual que una página que carga, así que un escritorio con "
    "segundero nunca está quieto. Pedir los rectángulos dañados en vez de sólo "
    "su existencia permitiría ignorar regiones chicas y periódicas.",
)

# --- el catálogo y la sala ----------------------------------------------------

review(
    "tool_search",
    "Ordenó el catálogo por keywords sobre nombre, categoría y descripción, con "
    "stopwords sacadas y unos veinte alias de categoría, y devolvió cada "
    "resultado con su schema y su riesgo para poder llamarlo sin un segundo "
    "round trip.",
    "Matching deliberadamente tonto, que es lo correcto para un corpus de 115 "
    "cadenas cortas — y aun así necesita que 'ssh' sea alcanzable desde la "
    "consulta, que es para lo que existen los alias. No puede contestar 'lo que "
    "escribe en un campo sin mover el mouse'. Rankear por los schemas que un "
    "modelo efectivamente llamó después, aprendido del action log, le ganaría a "
    "cualquier lista de alias escrita a mano.",
)
review(
    "action_log",
    "Devolvió el anillo de auditoría, y desde la etapa 1 cada entrada nombra la "
    "conexión y el cliente que hizo la llamada y lleva el tipo de denegación.",
    "En memoria y limitado a 2000 entradas, con JSONL sólo si se define "
    "ACTION_LOG. Un agente que quiera saber qué hizo hace una hora se encuentra "
    "con que ya no está. Hacer el archivo el default, rotado, no costaría nada y "
    "convertiría el log en algo en lo que apoyarse en vez de algo que hay que "
    "alcanzar a leer.",
)
review(
    "room_state",
    "Reportó quién está presente, quién tiene el control y si esta conexión "
    "puede inyectar — todo el estado de arbitraje en una lectura.",
    "La primitiva correcta para la invariante sobre la que está construido este "
    "proyecto. Lo que falta no está en esta herramienta sino al lado: no hay "
    "forma de que te avisen cuando cambia, así que un agente que pierde el "
    "control se entera porque lo rechazan. Eso es una notificación, no una "
    "lectura mejor.",
)

# --- tomar los controles ------------------------------------------------------

review(
    "request_control",
    "Le pidió el escritorio a la sala; se lo concedieron al instante porque "
    "nadie estaba manejando, y le habría puesto la pregunta a quienes miraban si "
    "hubiera habido alguien.",
    "Esto es el diseño funcionando: el control se reclama, nunca se asume, y "
    "preguntar es lo que hace visible cada traspaso. La mejora sería un campo de "
    "motivo que pueda llenar quien lo pide, para que el cartel que ve una "
    "persona diga para qué quiere el escritorio el agente y no solamente que lo "
    "quiere.",
)
review(
    "release_control",
    "Devolvió el escritorio, dejando los controles libres en vez de pasárselos a "
    "alguien.",
    "Correcto, incluida la parte que parece una omisión: no transferir significa "
    "que 'libre' es un estado en el que la sala puede quedarse, y nadie hereda "
    "un escritorio que no pidió. Una liberación diferida — devolver "
    "automáticamente después de N segundos sin actividad — evitaría que un "
    "agente que se cuelga a mitad de tarea se quede con los controles hasta que "
    "alguien lo note.",
)

# --- puntero y teclado --------------------------------------------------------

review(
    "mouse_move",
    "Movió el puntero por XTEST, el mismo camino que usa el DataChannel del "
    "navegador, así que una persona que esté mirando lo ve moverse.",
    "Un solo camino de inyección para los dos planos es justamente el punto: el "
    "puntero de un agente no es un segundo cursor invisible. Nada que mejorar a "
    "este nivel; suavizar un movimiento en pasos es cosa de quien llama.",
)
review(
    "mouse_click",
    "Hizo clic por XTEST, opcionalmente moviéndose primero, con botón y doble "
    "clic como argumentos.",
    "Correcto y completo para lo que un clic es. La parte débil es clickear a "
    "ciegas sobre coordenadas, y el arreglo no está acá: ui_click se dirige a un "
    "elemento y no puede errarle.",
)
review(
    "mouse_down",
    "Apretó y mantuvo un botón, dejando la pulsación abierta.",
    "Necesaria para todo lo que mouse_drag no puede expresar. También deja el "
    "escritorio en un estado apretado que sobrevive a la llamada, así que un "
    "agente que falla entre el down y el up deja el botón trabado — soltar los "
    "botones al liberar el control haría imposible ese estado irrecuperable.",
)
review(
    "mouse_up",
    "Soltó un botón que estaba apretado.",
    "El mismo problema de emparejamiento visto desde el otro lado: nada "
    "garantiza que llegue a correr. Ver mouse_down.",
)
review(
    "mouse_drag",
    "Apretó, movió y soltó como una sola llamada, que es lo que hace que un "
    "arrastre sea un arrastre y no tres eventos compitiendo.",
    "Correcto que sea una sola llamada. Se mueve en línea recta a una sola "
    "velocidad, y algunas interfaces distinguen un flick de un arrastre por la "
    "velocidad — una duración o cantidad de pasos opcional cubriría esos casos "
    "sin complicar el común.",
)
review(
    "mouse_scroll",
    "Scrolleó sintetizando pulsaciones de los botones 4 y 5, que es como X "
    "expresa la rueda.",
    "Botón 4/5 es la codificación vieja; los toolkits modernos esperan smooth "
    "scrolling de XInput2, y una aplicación que sólo escuche eso no se mueve. "
    "Mandar eventos de smooth scroll con fallback a botones arreglaría las "
    "aplicaciones que hoy no puede scrollear.",
)
review(
    "type_text",
    "Escribió el texto por el inyector, remapeando keycodes al vuelo para los "
    "caracteres que el layout activo no tiene en ninguna tecla.",
    "Ese remapeo es lo que la hace funcionar con acentos y símbolos en vez de "
    "sólo ASCII, y es el motivo para preferirla antes que componer llamadas a "
    "key_combo. Para un texto largo sigue siendo un evento de tecla sintético "
    "por carácter; poner el portapapeles y pegar sería más rápido, pero cambia "
    "lo que ve el escritorio, así que este es el default honesto.",
)
review(
    "key_combo",
    "Apretó una combinación por nombre de keysym de X, resolviendo cada nombre "
    "contra el keymap vivo.",
    "Nombrar teclas por keysym es lo correcto — es la capa que sobrevive a un "
    "cambio de layout. Un keysym que el mapa actual no tenga se descarta "
    "silenciosamente, que es cómo una combinación puede no hacer nada; rechazar "
    "la llamada nombrando el que falta convertiría una falla muda en un mensaje.",
)

# --- lanzar, ejecutar, grabar -------------------------------------------------

review(
    "launch_app",
    "Arrancó un programa desacoplado con setsid, así que cerrar la conexión MCP "
    "no se lleva puesta la aplicación, y con as_root pasando por sudo -E para "
    "conservar DISPLAY.",
    "Desacoplar es lo correcto y el motivo está escrito. Lo que no puede decir "
    "es si el programa realmente arrancó: vuelve apenas la shell hizo fork, y "
    "un comando que muere al instante se ve idéntico a uno que anduvo. "
    "open_app_and_wait existe por ese hueco, lo que ya indica que esta debería "
    "al menos devolver el pid y si seguía vivo un momento después.",
)
review(
    "activate_window",
    "Enfocó y trajo al frente una ventana por id usando wmctrl.",
    "Otro shell-out para un solo mensaje EWMH. Mandar _NET_ACTIVE_WINDOW "
    "directo sería una llamada X sin proceso y sin salida que parsear, y además "
    "permitiría reportar si el gestor de ventanas atendió el pedido — cosa que "
    "el código de salida de wmctrl no distingue de haber preguntado por una "
    "ventana que ya no existe.",
)
review(
    "run_command",
    "Corrió un comando por sh -c con deadline, capturando stdout, stderr y el "
    "código de salida, matando el proceso cuando se cancela la llamada y "
    "reportando su salida como progreso mientras corre.",
    "Todo lo que una herramienta de shell debería ser: un deadline real, un kill "
    "real, y la salida en streaming en vez de retenida hasta el final. Es la "
    "herramienta más peligrosa del catálogo y está clasificada como tal. Lo "
    "único que falta es una forma argv al lado de la de string, para poder pasar "
    "un nombre de archivo con espacios sin pensar en comillas.",
)
review(
    "start_recording",
    "Arrancó una grabación lanzando gst-launch-1.0 en su propio grupo de "
    "procesos, capturando y codificando la pantalla una segunda vez en paralelo "
    "al stream en vivo.",
    "El segundo encode es una decisión deliberada, no desperdicio, y medirlo lo "
    "confirma. Leer el framebuffer dos veces cuesta 1% de un núcleo: la captura "
    "es casi gratis y el codificador es toda la cuenta. Hacer tee del H.264 en "
    "vivo como hace start_restream dejaría la grabación casi gratis, pero "
    "heredaría el espaciado de keyframes del stream (10s, contra los 2s que "
    "quiere un archivo para poder buscar) y su bitrate, que el estimador de "
    "congestión ata a la peor red de los espectadores. Un archivo cuya calidad "
    "depende de quién estaba mirando es el default equivocado; corresponde "
    "detrás de una opción explícita para quien prefiera la CPU. Lo que sí "
    "estaba mal era todo aquello sobre lo que nadie había expresado una "
    "preferencia. ximagesrc entrega BGRx y x264enc acepta Y444 igual que I420, "
    "así que videoconvert elegía la conversión más barata para él y cada "
    "grabación salía en High 4:4:4 Predictive: el doble de croma para "
    "codificar, en un perfil que casi ningún decodificador por hardware acepta, "
    "de modo que el archivo se reproducía por software o directamente no se "
    "reproducía en los dispositivos más propensos a abrirlo. La cantidad de "
    "hilos tenía la misma forma: el default de x264enc se deduce de los núcleos "
    "del host y está dimensionado para terminar cada cuadro rápido, cosa que a "
    "un archivo en disco no le sirve. Juntas, en un host de 20 núcleos grabando "
    "una terminal con texto en movimiento, costaban 281% de un núcleo contra "
    "98% con el formato fijado en I420 y los hilos en 2 — los mismos cuadros a "
    "la salida, y ahora un archivo que se reproduce en cualquier lado. Queda el "
    "hijo gst-launch: GStreamer corre in-process en todos lados menos acá, y un "
    "proceso hijo significa sin bus, así que un pipeline que falla a mitad de "
    "la grabación es indistinguible de uno que anda.",
)
review(
    "stop_recording",
    "Señalizó el grupo de procesos del pipeline para que el contenedor se cierre "
    "correctamente, y reportó ruta y tamaño.",
    "Parar limpio en vez de matar es lo que hace que el mp4 sea reproducible, y "
    "ese detalle es fácil de errar. Por poco lo erraba: la espera a que drene "
    "el hijo sondeaba cmd.ProcessState mientras la goroutine que lo cosecha lo "
    "escribía, o sea las dos compitiendo, y perder esa carrera significaba que "
    "Stop no viera una salida que ya había ocurrido y matara a gst en medio de "
    "escribir el índice — produciendo justo el archivo irreproducible que el "
    "SIGINT existe para evitar. Ahora espera en un canal que cierra el "
    "cosechador. Queda que la espera exista: nada acá puede distinguir un "
    "pipeline que todavía está vaciando de uno colgado, porque un hijo "
    "gst-launch no ofrece un bus al que preguntarle.",
)
review(
    "get_recording_status",
    "Reportó si hay una grabación corriendo con segundos transcurridos, tamaño "
    "actual y ruta.",
    "El tamaño en disco es un buen proxy de 'realmente está escribiendo', que un "
    "booleano no daría. No puede decir si se están perdiendo cuadros, y una "
    "grabación que corre pero se está quedando sin datos es indistinguible de "
    "una sana hasta que la reproducís.",
)
review(
    "list_recordings",
    "Listó los archivos terminados con tamaño y fecha de modificación.",
    "La forma correcta. Reporta lo que hay en disco y no lo que es reproducible "
    "— un archivo que dejó una grabación que nunca paró limpio se ve igual que "
    "uno bueno. Sondear la duración del contenedor los separaría.",
)

# --- portapapeles -------------------------------------------------------------

review(
    "get_clipboard",
    "Leyó la selección CLIPBOARD de X con xclip, tratando un portapapeles vacío "
    "y una selección sin dueño como cosas normales y no como errores.",
    "La distinción que hace es correcta: que nadie sea dueño de la selección no "
    "es una falla. Pero es un subproceso por lectura para algo que la conexión X "
    "puede hacer, y sólo texto — una imagen o una ruta de archivo en el "
    "portapapeles le son invisibles, que es justo lo que más probablemente tenga "
    "una persona que copió algo para el agente.",
)

# --- ventanas -----------------------------------------------------------------

review(
    "wait_for_window",
    "Se bloqueó en un evento de X hasta que existiera una ventana cuyo título o "
    "clase coincida, con deadline, y corta cuando se cancela la llamada.",
    "Antes hacía polling: wmctrl cada 300ms, unos cincuenta procesos a lo largo "
    "de una espera de quince segundos para que le dijeran cuarenta y nueve "
    "veces que no había pasado nada, y con la respuesta llegando hasta un "
    "tercio de segundo tarde. El gestor de ventanas venía publicando el cambio "
    "en _NET_CLIENT_LIST todo ese tiempo. Ahora se suscribe con "
    "PropertyChangeMask a la ventana raíz y se bloquea, medido con la misma "
    "cantidad de procesos que un sleep de la misma duración — 33 lanzamientos "
    "pasaron a cero — y con la detección dentro del ruido del propio arranque "
    "de la ventana. Queda un límite honesto, y es por lo que hay un tick de "
    "respaldo de un segundo al lado de los eventos: una ventana ya abierta que "
    "se renombra escribe _NET_WM_NAME en su propia ventana, no en la raíz, así "
    "que un observador de la raíz no puede verlo. Cubrir eso implica seguir la "
    "lista de clientes, suscribirse en cada ventana nueva y manejar las que se "
    "destruyen en el medio.",
)
review(
    "move_window",
    "Movió una ventana con wmctrl -e.",
    "Shell-out y un string de geometría. Tampoco puede expresar 'mover sin "
    "redimensionar' salvo pasando centinelas -1 internamente, que es señal de "
    "que la llamada de abajo tiene la forma equivocada. Un ConfigureWindow "
    "directo toma los campos que se están fijando y nada más.",
)
review(
    "resize_window",
    "Redimensionó por el mismo camino de wmctrl -e.",
    "Igual que move_window, y comparte su helper. Conviene arreglarlas juntas y "
    "no por separado: una sola llamada directa de geometría reemplaza a las dos.",
)
review(
    "minimize_window",
    "Minimizó con xdotool windowminimize.",
    "Una segunda herramienta de shell para lo que las demás hacen con wmctrl, "
    "así que la familia ahora depende de dos programas externos para hacer un "
    "solo tipo de cosa. _NET_WM_STATE_HIDDEN por el mismo camino que el resto "
    "sacaría la dependencia de xdotool por completo.",
)
review(
    "maximize_window",
    "Agregó los estados EWMH maximized_vert y maximized_horz con wmctrl.",
    "Mecanismo correcto — le pide al gestor de ventanas en vez de redimensionar "
    "al tamaño de la pantalla, así que una ventana maximizada sigue maximizada "
    "cuando cambia la resolución. Sólo el shell-out la separa de un cinco.",
)
review(
    "restore_window",
    "Sacó los dos estados de maximizado.",
    "La inversa correcta de maximizar, y no intenta recordar una geometría "
    "previa que el gestor de ventanas ya conoce. Misma salvedad del shell-out.",
)
review(
    "fullscreen_window",
    "Alternó _NET_WM_STATE_FULLSCREEN.",
    "Alternar es el problema: un agente que no puede ver el estado actual no "
    "sabe para qué lado fue, así que 'poné esto en pantalla completa' requiere "
    "una lectura y una adivinanza. Debería tomar el estado que quiere — add, "
    "remove o toggle — como ya hace window_set_state.",
)
review(
    "set_window_desktop",
    "Movió una ventana a un escritorio virtual con wmctrl -t.",
    "Mecanismo correcto, y acepta el índice de escritorio que reportan las otras "
    "herramientas, así que los números coinciden en toda la familia. Otra vez el "
    "shell-out.",
)
review(
    "switch_desktop",
    "Cambió el escritorio virtual actual con wmctrl -s.",
    "Igual. Lo que valdría agregar es cambiar por nombre y no por índice, dado "
    "que _NET_DESKTOP_NAMES ya es lo que lee list_desktops.",
)
review(
    "window_properties",
    "Leyó las propiedades X crudas de una ventana.",
    "Esta es la única de la familia que va a la fuente, y es la más útil de "
    "todas para un agente que intenta entender una ventana que no abrió él. "
    "Nada que cambiar.",
)
review(
    "window_hierarchy",
    "Recorrió el árbol de ventanas de X, reportando padres, hijos y "
    "override-redirect.",
    "La respuesta correcta para preguntas que la lista EWMH no puede expresar — "
    "tooltips, menús y popups son override-redirect y nunca aparecen en "
    "list_windows. Combinarla con el árbol de accesibilidad sería el paso "
    "siguiente, pero eso es una herramienta nueva y no un cambio a esta.",
)
review(
    "window_set_state",
    "Fijó un estado EWMH — above, below, sticky, shaded, skip_taskbar y el resto "
    "— con un add, remove o toggle explícito.",
    "La herramienta mejor formada de la familia de ventanas: nombra el estado y "
    "la acción en vez de esconder los dos detrás de un verbo, que es lo que "
    "fullscreen_window debería haber hecho. Podría reemplazar de plano a "
    "maximize, restore y fullscreen, dejando una herramienta donde hay cuatro.",
)

# --- el árbol de accesibilidad ------------------------------------------------

review(
    "ui_tree",
    "Leyó el escritorio por AT-SPI como roles, nombres, estados y coordenadas — "
    "estructura en vez de píxeles — filtrado a la parte accionable.",
    "El instrumento correcto, y el motivo por el que read_screen_text saca un "
    "dos. Lo que la deja lejos del cinco es el costo: cada llamada ui_* levanta "
    "python3 e importa pyatspi, o sea unos cientos de milisegundos de proceso "
    "antes de que empiece el trabajo. a11y.py debería ser un demonio chico "
    "sosteniendo una sola conexión AT-SPI, como ya lo es el socket MCP.",
)
review(
    "ui_find",
    "Buscó por rol, nombre o texto y devolvió cada coincidencia con su ref, sus "
    "acciones, sus estados y coordenadas de pantalla.",
    "Devolver coordenadas junto al ref es lo que permite caer a un clic cuando "
    "falta una acción, y eso es buen diseño. También debería decir qué "
    "interfaces AT-SPI implementa cada elemento: este barrido encontró a "
    "Chromium reportando un campo como `editable` sin implementar EditableText, "
    "así que ui_set_text falló sobre algo que parecía escribible. Eso se puede "
    "saber antes de llamar, y sólo esta herramienta puede decirlo.",
)
review(
    "ui_get_text",
    "Leyó el texto de un elemento por ref, directo de la interfaz de "
    "accesibilidad.",
    "Exacto donde el OCR es probable, y barato donde una captura no lo es. "
    "Mismo costo de Python por llamada que el resto de la familia.",
)
review(
    "ui_click",
    "Invocó la acción propia de un elemento por ref. El puntero no se mueve, así "
    "que no puede errarle, y no importa si la ventana está parcialmente tapada.",
    "Este es el mejor enfoque para hacer clic que hay en el catálogo — "
    "mouse_click sobre coordenadas es una adivinanza al lado — y por eso mismo. "
    "Sólo el subproceso por llamada lo separa del cinco. También podría reportar "
    "qué acción invocó cuando un elemento tiene varias, porque 'la primera' es "
    "una decisión que hoy quien llama no puede ver.",
)
review(
    "ui_set_text",
    "Escribió texto en un campo por ref a través de AT-SPI, sin depender de qué "
    "ventana tiene el foco.",
    "La interfaz correcta, y mejor que tipear cuando funciona. Lo que no puede "
    "hacer es avisarte de antemano que no va a funcionar: Chromium expone sus "
    "campos como editables y no implementa EditableText, así que esta rechaza "
    "correctamente y quien llama se entera recién al intentar. Publicar la lista "
    "de interfaces en ui_find lo arregla allá y no acá. Dentro de una página, la "
    "respuesta es browser_type.",
)
review(
    "ui_focus",
    "Le dio el foco del teclado a un elemento por ref.",
    "El complemento correcto de type_text — enfocar por estructura y después "
    "escribir — y evita el baile de clickear para enfocar que mueve el puntero a "
    "un lugar que el usuario no esperaba. Mismo costo de subproceso.",
)
review(
    "ui_wait_for",
    "Se registró a los eventos de AT-SPI que pueden hacer aparecer un elemento y "
    "buscó cuando alguno se disparó, agrupando las ráfagas.",
    "Antes recorría el árbol de accesibilidad de cada aplicación abierta cuatro "
    "veces por segundo y filtraba el resultado. Cada nodo de ese recorrido es "
    "un viaje por D-Bus, y con una página real abierta una travesía son 289 "
    "nodos y 0.22s — así que el bucle ni siquiera podía sostener su propia "
    "cadencia: 250ms de sueño más 220ms de recorrido significaban que una "
    "espera de quince segundos gastaba cerca de un tercio de núcleo "
    "preguntando unas treinta veces si algo había cambiado. Escuchar cuesta "
    "una búsqueda al principio y después nada; el daemon del registro anota "
    "los mismos cero jiffies durante una espera de diez segundos que estando "
    "quieto. Las ráfagas se agrupan en vez de descartarse, porque una "
    "aplicación dibujándose emite cientos de children-changed en pocos "
    "milisegundos y el último es el que más probablemente importa. Lo que la "
    "mejoraría ahora es acotar la búsqueda al subárbol del propio evento: el "
    "elemento que apareció casi siempre está debajo del nodo que lo anunció, "
    "así que la travesía completa en cada despertar sigue siendo más de lo que "
    "la pregunta necesita.",
)
review(
    "ui_diff",
    "Devolvió sólo lo que cambió en el árbol desde la última llamada, guardando "
    "la instantánea previa del lado del servidor.",
    "La mejor respuesta del catálogo al problema que realmente limita a un "
    "agente: el contexto. Un ui_tree completo después de cada acción es la mayor "
    "parte del presupuesto de un modelo gastada en releer lo que ya sabía. "
    "Guardar la instantánea de este lado es lo que lo hace posible. Nada que "
    "cambiar.",
)

# --- terminal -----------------------------------------------------------------

review(
    "terminal_open",
    "Abrió un emulador de terminal en el escritorio, visible para quien esté "
    "mirando, con una shell que reporta su código de salida.",
    "El punto no es que un agente necesite una terminal — para eso está "
    "run_command — sino que una persona mirando pueda ver qué está haciendo. Esa "
    "es una decisión de producto que vale el costo. No puede reutilizar una "
    "terminal que abrió una persona, así que agente y persona terminan con dos.",
)
review(
    "terminal_run",
    "Tipeó un comando en la terminal con xdotool, esperó a que volviera el "
    "prompt y reportó el código de salida — contando las terminales primero para "
    "que un comando que cierra la shell no se espere hasta el timeout.",
    "El cuidado que tiene es real: xdotool en vez de XTEST crudo porque remapea "
    "keycodes para los caracteres de los que están llenas las líneas de comando, "
    "y el chequeo de cantidad de terminales existe porque un ref posicional "
    "empieza a resolver a otra ventana en silencio. Pero sigue siendo tipear en "
    "una pantalla y leer un prompt de vuelta. Hacer eco de un centinela con el "
    "código de salida y esperar ese string exacto sacaría la heurística del "
    "prompt por completo.",
)
review(
    "terminal_read",
    "Leyó el texto visible de la terminal por el árbol de accesibilidad.",
    "Leer el texto propio del emulador en vez de hacer OCR de sus píxeles es la "
    "elección correcta y el motivo por el que esto es usable. Ve sólo lo que "
    "está en pantalla, así que la salida que scrolleó ya no está — que es la "
    "diferencia entre esta y shell_read.",
)

# --- navegador ----------------------------------------------------------------

review(
    "browser_open",
    "Arrancó Chromium con el puerto de depuración, sondeó hasta que CDP "
    "respondió, y después esperó a que terminara de cargar la página pedida.",
    "El sondeo está bien acá y en ningún otro lugar de las herramientas de "
    "navegador: antes de que un proceso empiece a escuchar no hay evento que "
    "esperar, porque el socket que lo llevaría es justamente lo que se está "
    "esperando. Lo que estaba mal era el final — informaba el navegador "
    "abierto mientras la página que le habían pasado por línea de comandos "
    "seguía cargando. Preguntarle al documento resuelve eso sin carrera, "
    "porque uno ya completo resuelve al instante y uno cargando resuelve con "
    "su propio evento de carga. Los cuarenta segundos de sondeo todavía se "
    "pueden acortar: Chromium escribe su endpoint de DevTools en un archivo "
    "del directorio de perfil cuando está listo, y vigilar eso convertiría el "
    "último sondeo de este archivo en una respuesta.",
)
review(
    "browser_tabs",
    "Listó los targets abiertos por el endpoint HTTP de CDP.",
    "La fuente autoritativa — esto es lo que el navegador dice de sí mismo. Abre "
    "un cliente HTTP nuevo por llamada, que acá es barato pero es parte de un "
    "patrón que comparte toda la familia.",
)
review(
    "browser_goto",
    "Navegó con Page.navigate y volvió recién cuando se disparó el evento de "
    "carga.",
    "Antes asignaba location.href y contestaba 'navigating' — cierto en el "
    "instante en que se decía y viejo para cuando alguien lo leía, así que "
    "toda herramienta llamada después corría una carrera contra la página, y "
    "el arreglo habitual era que el modelo adivinara un sleep. La descripción "
    "ya afirmaba que esperaba la carga; ahora la espera. Page.navigate además "
    "expone lo que una asignación a href se traga: un esquema inválido, una "
    "URL bloqueada, un host que no resuelve. Espera el evento de carga o que "
    "el frame se detenga, lo que cubre una navegación respondida con una "
    "descarga, y reporta el timeout como una navegación sin confirmar y no "
    "como un fallo, porque la página suele estar. Una aplicación de una sola "
    "página que rutea sin cargar documento es el caso que todavía no ve.",
)
review(
    "browser_eval",
    "Evaluó JavaScript contra el DOM vivo por CDP.",
    "La herramienta más autoritativa de la familia del navegador: le pregunta a "
    "la página misma en vez de a la foto que alguien tenga de ella. Bien "
    "clasificada como peligrosa, porque es código arbitrario en el origen que "
    "esté cargado. Un WebSocket nuevo por llamada es el costo compartido de la "
    "familia, no un defecto de esta.",
)
review(
    "browser_click",
    "Llevó el elemento a la vista, comprobó qué hay realmente encima, y clickeó "
    "por Input.dispatchMouseEvent en su centro.",
    "el.click() despachaba un solo click sintético y nada más: sin movimiento "
    "del puntero, sin mousedown, sin mouseup, con isTrusted en falso. Medido "
    "sobre una página que registra los cuatro, ahora produce pointerdown, "
    "mousedown, mouseup y click, todos confiables. La reparación más útil es "
    "que ahora puede fallar: un click por DOM nunca pregunta qué hay encima, "
    "así que un botón debajo de un banner de cookies o de un modal se "
    "clickeaba igual y a quien llamaba se le decía que funcionó, mientras el "
    "banner se llevaba el click. elementFromPoint resuelve eso antes de "
    "despachar, y un elemento tapado se reporta por nombre. Queda pendiente: "
    "un elemento que necesita hover para volverse clickeable sigue "
    "necesitando un movimiento aparte, porque el de acá ocurre demasiado tarde "
    "para abrir nada.",
)
review(
    "browser_type",
    "Enfocó el campo, seleccionó lo que había e insertó el texto por "
    "Input.insertText, así el cambio viene de la capa de input del navegador.",
    "Antes asignaba el.value y disparaba un evento input sintético, que escribe "
    "algo que la página puede ver y nada que la página crea. Los frameworks "
    "que rastrean su propio valor reemplazan la propiedad value por un "
    "accessor y recuerdan lo último que vieron, así que la asignación "
    "actualiza el rastreador de paso y el evento sintético que sigue parece un "
    "no-cambio. Reproducido contra una página que implementa ese rastreo: el "
    "campo mostraba \"hello\", la página contaba cero cambios, y la "
    "herramienta reportaba éxito — así que un formulario se enviaría vacío y "
    "la validación nunca correría. Input.insertText entra donde entra una "
    "tecla, y esa misma página ahora cuenta el cambio. Verificado en input, "
    "textarea y contenteditable, reemplazando en vez de concatenar, y con "
    "texto vacío vaciando el campo. Lo que todavía no hace es emitir eventos "
    "de tecla por carácter, que un buscador que filtra mientras escribís o un "
    "campo con máscara pueden estar escuchando.",
)
review(
    "browser_text",
    "Leyó el texto visible de la página a través del DOM.",
    "Exacto donde hacer OCR de una ventana de navegador es adivinar, y respeta "
    "lo que efectivamente se renderizó y no lo que dice el markup. Nada que "
    "cambiar a este nivel.",
)
review(
    "browser_wait_for",
    "Esperó desde adentro de la página: un solo evaluate cuya promesa resuelve "
    "un MutationObserver en cuanto aparece un nodo que coincide.",
    "Antes le preguntaba al navegador cincuenta veces si el nodo había "
    "aparecido, y cada una de esas preguntas abría un WebSocket nuevo, "
    "corría una consulta y lo cerraba — un handshake completo tres veces por "
    "segundo para que le dijeran 'todavía no'. Un MutationObserver mueve la "
    "espera a donde ocurre el cambio, que es lo que hace Playwright y por la "
    "misma razón: la página ya lo sabe. Medido contra un elemento insertado "
    "cuatro segundos después de la carga devolvió a los 4.00s, y las "
    "conexiones al puerto de depuración se mantuvieron en dos durante toda la "
    "espera en vez de rotar. Queda el caso que el observador no sobrevive: "
    "una navegación a mitad de la espera destruye el contexto de ejecución y "
    "se lleva la promesa. Eso ahora se reporta como lo que es y no como un "
    "error de protocolo pelado, pero rearmarlo sobre el documento nuevo "
    "sería mejor que reportarlo.",
)

# --- archivos -----------------------------------------------------------------

review(
    "read_file",
    "Leyó un archivo con os.ReadFile, o por cat bajo sudo cuando se pide "
    "as_root, dado que el daemon corre sin privilegios.",
    "La división es exactamente la correcta: el camino común es una lectura "
    "directa sin proceso, y el privilegio es un pedido explícito en vez de algo "
    "que el daemon sostiene. max_bytes evita que quien llama se llene su propio "
    "contexto con un archivo de log. Nada que mejorar.",
)
review(
    "write_file",
    "Escribió un archivo directo, o por un ayudante privilegiado para as_root, "
    "con append y modo como opciones.",
    "La misma forma que read_file y el mismo razonamiento. Bien clasificada como "
    "peligrosa. Lo único que no puede hacer es escribir de forma atómica — una "
    "escritura parcial es visible para cualquiera que esté mirando el archivo — "
    "lo que importa para configuración que un servicio está leyendo.",
)
review(
    "list_directory",
    "Listó un directorio con nombres, tamaños, tipos y fechas de modificación.",
    "Directa, y devuelve los campos por los que si no habría que hacer una "
    "segunda llamada. Nada que cambiar.",
)

# --- acciones compuestas ------------------------------------------------------

review(
    "open_app_and_wait",
    "Lanzó un programa, esperó a que apareciera su ventana, la enfocó y esperó a "
    "que se asentara el dibujado — las cuatro llamadas que un agente haría, en "
    "una sola.",
    "Existe porque launch_app no puede decir si el programa arrancó, y comprimir "
    "cuatro round trips en uno vale contexto real. Hereda el polling de "
    "wait_for_window, así que arreglar aquello arregla esto.",
)
review(
    "fill_form",
    "Completó varios campos por nombre accesible y opcionalmente apretó un "
    "botón, reportando el éxito campo por campo.",
    "Reportar cada campo por separado en vez de un solo pasa/no pasa es lo "
    "correcto — un formulario donde tres de cuatro campos entraron es un "
    "problema distinto de uno que no hizo nada. Escribe por la misma interfaz "
    "AT-SPI que ui_set_text y hereda su limitación: en Chromium no puede, y "
    "tampoco puede avisarlo de antemano.",
)

# --- cerrar, matar ------------------------------------------------------------

review("close_window", "Le pidió al gestor de ventanas que cierre una ventana, con wmctrl.",
 "El cierre cortés — la aplicación puede preguntar por lo no guardado — y eso "
 "está bien. Shell-out como el resto de la familia, y no puede reportar si la "
 "ventana efectivamente se fue: un programa con diálogo de confirmación queda "
 "abierto y esto devuelve éxito igual.")
review("kill_process", "Terminó un proceso por nombre o pid, con force como opción.",
 "Señalizar en vez de usar siempre SIGKILL es el default correcto: un proceso "
 "que puede limpiar debería poder hacerlo. Coincidir por nombre tiene el mismo "
 "problema de substring que list_processes, así que 'sleep' puede terminar más "
 "de lo previsto — devolver qué coincidió antes de actuar lo haría visible.")

# --- joystick -----------------------------------------------------------------

review("gamepad_button", "Apretó o soltó un botón en un dispositivo uinput real.",
 "Un dispositivo virtual en el kernel, no eventos X sintéticos: la aplicación "
 "lo lee por evdev exactamente como leería un control enchufado, y no puede "
 "notar la diferencia. Es la respuesta más fuerte posible acá, y el motivo por "
 "el que toda la familia funciona en juegos que ignoran input falso.")
review("gamepad_tap", "Apretó y soltó con una espera en el medio.",
 "La comodidad que evita que quien llama tenga que cronometrar dos llamadas a "
 "través del cable, que es donde un toque se convierte en un mantenido. Bloquea "
 "mientras dura, así que un hold largo ocupa la llamada; ese es el canje honesto "
 "por precisión.")
review("gamepad_axis", "Movió un eje del stick por el mismo dispositivo uinput.",
 "Valores absolutos de eje sobre un dispositivo real. No existe nada mejor "
 "salvo un control físico.")
review("gamepad_state", "Fijó todos los botones y ejes en una sola llamada.",
 "La forma correcta para un loop de juego: una llamada por cuadro en vez de una "
 "docena, y una foto consistente en vez de una carrera entre eventos sueltos.")

# --- audio y retransmisión ----------------------------------------------------

review("set_volume", "Fijó el volumen o el silencio con pactl.",
 "Hace shell-out por llamada para algo que PulseAudio expone por su propio "
 "socket. Además fija el sink del que graba el escritorio, así que cambia lo que "
 "captura una grabación y no sólo lo que escucha alguien — vale decirlo en la "
 "descripción, porque son intenciones distintas.")
review("start_restream",
 "Enganchó un destino externo a la salida H.264 viva por el tee del pipeline, "
 "sin codificar nada por segunda vez.",
 "Este es el ejemplo que debería seguir el resto del camino de medios. Un "
 "segundo espectador cuesta ancho de banda, no CPU, y un segundo destino "
 "tampoco. Además está correctamente gateada por la sala: publicar lo que está "
 "en la pantalla de todos hacia afuera no es una decisión que un agente tome "
 "solo. start_recording es la herramienta que debería estar leyendo el fuente "
 "de ésta.")
review("stop_restream", "Desenganchó un destino del tee.",
 "Remoción limpia sin perturbar el encode vivo, que es lo que compra el tee. "
 "Parar por id cuando hay varios corriendo es lo correcto; pararlos todos "
 "requiere un loop que tiene que escribir quien llama.")
review("list_restreams", "Reportó dónde se está publicando el escritorio ahora.",
 "La respuesta de auditoría a una pregunta que importa — esta es la herramienta "
 "que dice si la pantalla está saliendo de la sala. Reporta los destinos pero no "
 "cómo van, así que un push trabado se ve igual que uno sano.")

# --- shells persistentes ------------------------------------------------------

review("shell_open", "Arrancó una shell sobre un PTY real, dimensionado en filas y columnas.",
 "Un pseudo-terminal en vez de un pipe, que es la diferencia entre una shell "
 "que se comporta y una que apaga su prompt, sus colores y su edición de línea "
 "porque cree que nadie la mira. Los programas interactivos funcionan acá por el "
 "mismo motivo.")
review("shell_exec", "Corrió un comando en una sesión abierta y esperó a que la salida se aquietara.",
 "Un período de silencio es la forma honesta de saber que una shell interactiva "
 "terminó cuando no hay código de salida que leer — y sigue siendo una "
 "heurística, así que un comando que hace una pausa a mitad de salida parece "
 "terminado. Hacer eco de un centinela con $? convertiría la adivinanza en un "
 "hecho, el mismo arreglo que necesita terminal_run.")
review("shell_input", "Mandó teclas crudas a una sesión sin esperar.",
 "Necesaria para todo lo que shell_exec no puede expresar — contestar un "
 "prompt, mandar Ctrl-C, manejar un programa de pantalla completa. Disparar y "
 "olvidarse es el punto, y significa que quien llama tiene que emparejarla con "
 "shell_read por su cuenta.")
review("shell_read", "Leyó y limpió todo lo que produjo la sesión desde la última lectura.",
 "Leer-y-limpiar es el contrato correcto para hacer polling de un comando largo: "
 "nada se entrega dos veces. También significa que la lectura de uno esconde esa "
 "salida de otro, lo que importa ahora que varios sub-agentes pueden compartir "
 "un escritorio.")
review("shell_list", "Listó las sesiones abiertas con su antigüedad y bytes pendientes.",
 "Reportar bytes pendientes es lo que la hace útil y no decorativa — una sesión "
 "con salida sin leer es una que alguien debería leer. No puede decir qué está "
 "corriendo en cada una.")
review("shell_close", "Terminó una sesión y liberó su PTY.",
 "Limpieza explícita, que importa porque un PTY y un proceso de shell sobreviven "
 "a la conexión MCP que los creó. Las sesiones no tienen timeout por "
 "inactividad, así que una olvidada vive hasta que el escritorio reinicie.")

# --- SSH ----------------------------------------------------------------------

review("ssh_connect",
 "Abrió una sesión con golang.org/x/crypto/ssh — el protocolo en Go, no el "
 "comando ssh manejado desde afuera.",
 "Esta es la diferencia entre sostener una conexión y rehacerla en cada llamada, "
 "y es el motivo por el que exec, sftp y túneles pueden compartir una sesión. El "
 "manejo de host keys es lo que hay que mirar antes de confiar en esto fuera de "
 "un contenedor: la comodidad ahí es donde el tooling de SSH suele fallar.")
review("ssh_exec", "Corrió un comando por la sesión abierta, devolviendo stdout, stderr y código de salida.",
 "Un canal sobre una conexión existente, así que cuesta un round trip y no un "
 "handshake, y el código de salida es el del protocolo y no algo parseado de "
 "vuelta. Nada que mejorar a este nivel.")
review("ssh_upload", "Mandó un archivo por SFTP sobre la misma conexión.",
 "SFTP por pkg/sftp en vez de shell-out a scp: sin segunda autenticación, sin "
 "tener que citar una ruta remota a través de una shell, y errores que nombran "
 "la operación. Lee el archivo local a memoria, que está bien para lo que mueve "
 "un agente y no para una imagen.")
review("ssh_download", "Trajo un archivo por la misma sesión SFTP.",
 "Mismo razonamiento y misma salvedad de memoria.")
review("ssh_list_remote", "Listó un directorio remoto por SFTP.",
 "Entradas estructuradas del protocolo en vez de salida de ls parseada, que es "
 "justo la trampa que esto evita — la salida de ls es para personas.")
review("ssh_tunnel_local", "Reenvió un puerto local al lado remoto por la sesión.",
 "Un canal reenviado real, administrado y cerrable, no un ssh -L en segundo "
 "plano que después nadie encuentra. El túnel pertenece a la sesión y muere con "
 "ella.")
review("ssh_tunnel_remote", "Reenvió un puerto remoto de vuelta hacia este lado.",
 "La dirección más difícil, y funciona igual. Si el sshd remoto lo permite es "
 "decisión del servidor, y el error lo dice.")
review("ssh_tunnels", "Listó los túneles de una sesión, con su cantidad de conexiones.",
 "La cantidad de conexiones la convierte en una respuesta operativa y no en un "
 "inventario. No puede decir si un túnel está fallando, sólo si algo lo usó.")
review("ssh_tunnel_close", "Cerró un túnel por id.",
 "La granularidad correcta — la sesión sobrevive. Las conexiones existentes se "
 "cortan sin forma de drenarlas primero, que es el default correcto y vale "
 "documentarlo.")
review("ssh_list", "Listó las sesiones SSH abiertas.",
 "El inventario que hace usables a las herramientas basadas en id después de "
 "reiniciar el cliente. Como shell_list, no dice nada sobre salud.")
review("ssh_disconnect", "Cerró una sesión y todo lo que colgaba de ella.",
 "Desarme explícito, y que los túneles se vayan con ella es el acoplamiento "
 "correcto.")
review("ssh_keygen", "Generó un par de claves corriendo ssh-keygen.",
 "crypto/ed25519 y x/crypto/ssh pueden generar y serializar una clave sin salir "
 "del proceso, lo que además permitiría negarse a sobrescribir sin parsear un "
 "prompt. Ya se niega a sobrescribir una clave existente, que es la parte "
 "importante.")
review("ssh_copy_id", "Agregó la clave pública al authorized_keys remoto.",
 "Arma un comando de shell y lo corre remoto, así que depende de que el remoto "
 "tenga una shell POSIX y de que el quoting sobreviva. Escribir el archivo por "
 "SFTP — leer, agregar, escribir, chmod — usa la conexión que esta herramienta "
 "ya sostiene y funciona en hosts con una shell inusual.")

# --- paquetes y servicios -----------------------------------------------------

review("sudo_status", "Reportó si esta imagen tiene sudo sin contraseña.",
 "Lo correcto para preguntar antes de ofrecerle a un agente un camino "
 "privilegiado, y barato. Contesta sobre la capacidad y no sobre un comando "
 "puntual, así que un sudoers restringido se ve igual que uno completo.")
review("install_packages",
 "Instaló con apt bajo deadline, reportando la salida del propio comando como "
 "progreso y matándolo al cancelar.",
 "Todo lo que debería ser el camino largo, y el progreso que emite es el texto "
 "propio de apt y no un spinner. No puede revertir una instalación parcial, para "
 "lo cual está snapshot_create — vale decirlo en la descripción, porque las dos "
 "van juntas.")
review("remove_packages", "Sacó paquetes, con purge como opción.",
 "Purge como elección explícita y no como default es lo correcto: la "
 "configuración es la parte que la gente pasa por alto. No reporta qué más "
 "sacaría apt como consecuencia, que es el número que importa antes de decir que "
 "sí.")
review("search_packages", "Buscó en apt sin instalar nada.",
 "Correctamente de sólo lectura, que es por lo que sobrevive a "
 "MCP_POLICY=readonly. Parsea la salida de apt pensada para personas, y apt dice "
 "explícitamente que su CLI no tiene una interfaz estable entre versiones. "
 "python-apt o la base de dpkg no se moverían debajo.")
review("service_control",
 "Le preguntó a supervisord por los programas del escritorio, y puede "
 "arrancarlos, pararlos o reiniciarlos.",
 "Hablar con el supervisor que realmente es dueño de esos procesos es lo "
 "correcto, y reconoce tanto la configuración del contenedor como la nativa. "
 "Parar el programa equivocado le saca el escritorio a todos, y la herramienta "
 "no distingue los que se pueden rebotar sin riesgo de los que no.")

# --- sistema ------------------------------------------------------------------

review("set_resolution",
 "Cambió el modo con xrandr, dentro del tamaño reservado cuando arrancó el "
 "display.",
 "Cambiar la resolución sin reiniciar nada es genuinamente útil, y el techo es "
 "honesto — Xvfb reserva su framebuffer al inicio, así que crecer más allá no es "
 "algo que esto pudiera arreglar. Reportar los modos disponibles dejaría elegir "
 "en vez de adivinar y que te rechacen.")

# --- snapshots ----------------------------------------------------------------

review("snapshot_create",
 "Empaquetó el home en un tar y registró la lista de paquetes instalados, "
 "excluyendo el propio directorio de snapshots para que no se aniden, y "
 "rechazando un resultado demasiado chico para ser real.",
 "Los dos chequeos muestran que alguien pensó cómo falla esto: excluirse a sí "
 "mismo evita el crecimiento cuadrático, y el chequeo de tamaño atrapa un tar "
 "que no empaquetó nada. Pero copia el home entero cada vez — sin incremental, "
 "sin deduplicación — así que el segundo snapshot cuesta lo mismo que el "
 "primero. Además corre mientras se escriben archivos, así que una base de datos "
 "en el home queda capturada a mitad de escritura.")
review("snapshot_list", "Listó los snapshots con su tamaño y fecha.",
 "Alcanza para elegir uno. No muestra qué cambiaría una restauración, que es la "
 "pregunta que alguien realmente tiene antes de restaurar.")
review("snapshot_restore",
 "Desempaquetó un snapshot sobre el home y reportó qué paquetes se instalaron "
 "después de tomarlo.",
 "Reportar la diferencia de paquetes en vez de revertirla en silencio es la "
 "parte buena — archivos y paquetes son tipos de estado distintos y no finge lo "
 "contrario. Desempaquetar sobre el home vivo deja en su lugar todo lo creado "
 "desde entonces, así que una restauración es una fusión y no la vuelta atrás "
 "que sugiere el nombre. Decir primero qué archivos va a sobrescribir la "
 "convertiría en algo que una persona puede aceptar.")
review("snapshot_delete", "Borró un snapshot y su lista de paquetes.",
 "Saca las dos mitades, que es la falla a evitar — una lista de paquetes sin su "
 "tar es peor que nada. Sin confirmación, correctamente: eso le corresponde a "
 "quien llama.")

review(
    "list_windows",
    'Listó cada ventana con id, escritorio, geometría, clase y título, leídos directo de _NET_CLIENT_LIST y de las propiedades de cada ventana.',
    'Antes hacía shell-out a wmctrl y partía la salida por espacios, así que una ventana llamada "Report  2026" — dos espacios — se parseaba como otra ventana con otra geometría. internal/desktop/ewmh.go lee las propiedades que X ya tiene: sin subproceso, sin locale, sin aritmética de columnas. Lo único que queda es avisarle a quien llama cuando el gestor de ventanas no publica ninguna lista de clientes, que es una falla distinta de un escritorio vacío.',
)

review(
    "list_desktops",
    'Listó los escritorios virtuales y marcó el actual, desde _NET_NUMBER_OF_DESKTOPS, _NET_CURRENT_DESKTOP y _NET_DESKTOP_NAMES.',
    'Esto antes no era sólo poco elegante, estaba mal. El parser viejo tomaba todos los campos desde el índice 8 como nombre, así que cada escritorio volvía llamándose "1920x1044 desktop 1", con el tamaño del área de trabajo pegado adelante — desde que la herramienta existía, porque nadie leyó la salida de cerca. Leer la propiedad de nombres da el nombre.',
)

review(
    "get_active_window",
    'Leyó _NET_ACTIVE_WINDOW y describió esa ventana: id, geometría, clase y título como campos.',
    'Una lectura de propiedad donde antes eran tres procesos xdotool devolviendo un párrafo de texto para parsear. Que no haya foco ahora es una respuesta con nota y no un error, así que quien llama puede distinguir un escritorio inactivo de una consulta rota. Las coordenadas se traducen a la raíz, así que son las que sirven para un clic incluso con un gestor de ventanas que reparenta.',
)

review(
    "set_clipboard",
    'Escribió texto en la selección CLIPBOARD de X y reportó si la escritura realmente ocurrió.',
    'Antes descartaba el resultado, así que una escritura fallida se reportaba como éxito y el agente iba a pegar algo que nunca estuvo. Acertarle al arreglo llevó tres intentos: capturar stderr hizo que Go creara un pipe que el hijo demonizado de xclip heredó y nunca cerró, y colgó sesenta segundos; agregar WaitDelay arregló eso e hizo que una escritura exitosa se reportara como rota, porque ErrWaitDelay es el hijo sosteniendo el pipe y no un comando fallido. Sigue siendo un subproceso por escritura, y sigue siendo sólo texto — ser dueño de la selección desde adentro del daemon es lo que lo llevaría a cinco.',
)


# --- revised after the window tools were rewritten against X -------------------
#
# review() assigns into a dict, so an entry here replaces the one above. Kept as
# an addition rather than an edit in place: the earlier text described a real
# version of these tools, and the two together say what changed and why. When
# the older half stops being interesting, delete it — do not silently overwrite
# the record of what the code used to be.

review(
    'activate_window',
    'Enfocó y trajo al frente una ventana con un mensaje de cliente _NET_ACTIVE_WINDOW.',
    'Pedirle al gestor de ventanas en vez de levantar la ventana a sus espaldas, que es lo que la hace funcionar con un gestor que reparenta y decora. El mensaje lleva una indicación de origen, así que las reglas de robo de foco tratan a un agente como a cualquier cosa que pidió el usuario.',
)

review(
    'move_window',
    'Movió una ventana con _NET_MOVERESIZE_WINDOW, marcando sólo los campos que fija.',
    "Los flags son lo que hace expresable 'mover sin redimensionar': el camino viejo armaba un string de geometría y pasaba centinelas -1 que el gestor después tenía que ignorar. Verificado contra un escritorio real — un move seguido de un resize conserva la posición que se le dio.",
)

review(
    'resize_window',
    'Redimensionó por el mismo mensaje, dejando la posición en paz.',
    'Comparte MoveResize con move_window, que es lo correcto: una llamada que dice qué campos quiere decir le gana a dos que fingen fijar todo.',
)

review(
    'close_window',
    'Le pidió a la ventana que se cierre con _NET_CLOSE_WINDOW — el pedido propio del botón de la barra de título.',
    "El cierre cortés: la aplicación puede guardar, objetar o poner un diálogo. Sigue sin poder reportar si la ventana efectivamente se fue, porque es un pedido y la respuesta llega después. Esperar un momento y releer la lista de clientes convertiría 'pedido' en 'cerrada'.",
)

review(
    'minimize_window',
    'Iconificó con un mensaje WM_CHANGE_STATE llevando IconicState.',
    'El pedido correcto, y la parte sutil: _NET_WM_STATE_HIDDEN es lo que un gestor FIJA para reportar que una ventana está minimizada, no algo que un cliente pide. Usarlo habría sido la respuesta equivocada más plausible. Esto además sacó la última dependencia de xdotool de la familia de ventanas.',
)

review(
    'maximize_window',
    'Agregó los dos estados de maximizado en un solo mensaje _NET_WM_STATE.',
    'Dos estados por mensaje es lo que permite el protocolo y lo que este caso necesita — un cambio sobre el que el gestor puede actuar, en vez de dos que tiene que reconciliar. Restore devolvió la ventana a la geometría exacta que tenía antes, porque el gestor la recuerda y esto pidió en vez de redimensionar.',
)

review(
    'restore_window',
    'Sacó los dos estados de maximizado.',
    'La inversa correcta, y no intenta recordar una geometría que el gestor de ventanas ya tiene.',
)

review(
    'fullscreen_window',
    'Fijó, quitó o alternó _NET_WM_STATE_FULLSCREEN, con action por defecto en toggle.',
    'Antes sólo alternaba, así que un agente que quería una ventana en pantalla completa tenía que leer el estado y adivinar para dónde iba el toggle. Nombrar la acción lo arregla sin cambiarle nada a quien ya la llamaba.',
)

review(
    'set_window_desktop',
    'Movió una ventana a un escritorio virtual con _NET_WM_DESKTOP.',
    'Nativo, y los índices coinciden con lo que reporta list_desktops porque ahora las dos leen las mismas propiedades.',
)

review(
    'switch_desktop',
    'Cambió el escritorio actual con _NET_CURRENT_DESKTOP.',
    'Nativo. Lo que sigue faltando es cambiar por nombre — list_desktops ya lee _NET_DESKTOP_NAMES, así que los nombres existen y sólo esta punta no puede tomar uno.',
)
